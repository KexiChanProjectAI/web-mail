import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { ForwardableEmailMessage } from '@cloudflare/workers-types';

const mockFetch = vi.fn();
(globalThis as Record<string, unknown>).fetch = mockFetch;

const mockConsoleLog = vi.fn();
const mockConsoleError = vi.fn();
vi.stubGlobal('console', {
  log: mockConsoleLog,
  error: mockConsoleError,
});

interface MockContext {
  waitUntil: (promise: Promise<unknown>) => void;
  passThroughOnException: () => void;
}

const createMockContext = (): MockContext => {
  const waitUntilFn = vi.fn((promise: Promise<unknown>) => {
    promise.catch(() => {});
  });
  return {
    waitUntil: waitUntilFn,
    passThroughOnException: vi.fn(),
  };
};

const createMockMessage = (overrides: Partial<{
  from: string;
  to: string;
  raw: ReadableStream;
  rawSize: number;
  headers: Headers;
  canBeForwarded: boolean;
}> = {}): ForwardableEmailMessage => {
  const rawContent = 'From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Test\r\n\r\nTest body';
  const encoder = new TextEncoder();
  const rawStream = new ReadableStream({
    start(controller) {
      controller.enqueue(encoder.encode(rawContent));
      controller.close();
    },
  });

  return {
    from: 'sender@example.com',
    to: 'recipient@example.com',
    raw: rawStream,
    rawSize: rawContent.length,
    headers: new Headers({
      'subject': 'Test Email',
      'message-id': '<test-message-id@example.com>',
      'date': 'Tue, 19 May 2026 12:00:00 +0000',
    }),
    canBeForwarded: true,
    setReject: vi.fn(),
    forward: vi.fn().mockResolvedValue({ uuid: 'test-uuid' }),
    reply: vi.fn().mockResolvedValue({ uuid: 'test-uuid' }),
    ...overrides,
  } as ForwardableEmailMessage;
};

interface Env {
  INGEST_URL: string;
  WORKER_INGEST_PSK: string;
}

const createMockEnv = (overrides: Partial<Env> = {}): Env => ({
  INGEST_URL: 'https://mail.example.com/ingest',
  WORKER_INGEST_PSK: 'test-psk-secret',
  ...overrides,
});

describe('Email Worker', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockFetch.mockReset();
    mockConsoleLog.mockReset();
    mockConsoleError.mockReset();
  });

  describe('email handler', () => {
    it('should POST raw MIME to ingest endpoint with correct headers', async () => {
      const { email } = await import('../src/index');

      const message = createMockMessage({
        from: 'sender@example.com',
        to: 'catchall@lite-mail.example.com',
        rawSize: 100,
      });
      const env = createMockEnv({
        INGEST_URL: 'https://mail.example.com/ingest',
        WORKER_INGEST_PSK: 'super-secret-psk',
      });
      const ctx = createMockContext();

      mockFetch.mockResolvedValueOnce(
        new Response(null, { status: 200, statusText: 'OK' })
      );

      await email(message, env, ctx);

      expect(mockFetch).toHaveBeenCalledTimes(1);

      const [url, options] = mockFetch.mock.calls[0];

      expect(url).toBe('https://mail.example.com/ingest');
      expect(options.method).toBe('POST');
      expect(options.headers).toEqual({
        'Content-Type': 'message/rfc822',
        'X-Lite-Mail-Ingest-PSK': 'super-secret-psk',
      });

      expect(options.body).toBeInstanceOf(ArrayBuffer);
      expect(ctx.waitUntil).toHaveBeenCalledTimes(1);
    });

    it('should read raw MIME stream correctly', async () => {
      const { email } = await import('../src/index');

      const expectedMime = 'From: test@test.com\r\nTo: you@yours.com\r\nSubject: Hello\r\n\r\nContent';
      const encoder = new TextEncoder();
      const rawStream = new ReadableStream({
        start(controller) {
          controller.enqueue(encoder.encode(expectedMime));
          controller.close();
        },
      });

      const message = createMockMessage({
        from: 'test@test.com',
        to: 'you@yours.com',
        raw: rawStream,
        rawSize: expectedMime.length,
      });
      const env = createMockEnv();
      const ctx = createMockContext();

      mockFetch.mockResolvedValueOnce(
        new Response(null, { status: 200 })
      );

      await email(message, env, ctx);

      const [, options] = mockFetch.mock.calls[0];
      const decoder = new TextDecoder();
      const sentBody = decoder.decode(options.body as ArrayBuffer);

      expect(sentBody).toBe(expectedMime);
    });

    it('should call setReject when INGEST_URL is missing', async () => {
      const { email } = await import('../src/index');

      const message = createMockMessage();
      const env = createMockEnv({
        INGEST_URL: '',
        WORKER_INGEST_PSK: 'valid-psk',
      });
      const ctx = createMockContext();

      await email(message, env, ctx);

      expect(message.setReject).toHaveBeenCalledWith(
        'Worker misconfigured: missing ingest URL or PSK'
      );
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should call setReject when WORKER_INGEST_PSK is missing', async () => {
      const { email } = await import('../src/index');

      const message = createMockMessage();
      const env = createMockEnv({
        INGEST_URL: 'https://mail.example.com/ingest',
        WORKER_INGEST_PSK: '',
      });
      const ctx = createMockContext();

      await email(message, env, ctx);

      expect(message.setReject).toHaveBeenCalledWith(
        'Worker misconfigured: missing ingest URL or PSK'
      );
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should handle empty raw stream', async () => {
      const { email } = await import('../src/index');

      const emptyStream = new ReadableStream({
        start(controller) {
          controller.close();
        },
      });

      const message = createMockMessage({
        raw: emptyStream,
        rawSize: 0,
      });
      const env = createMockEnv();
      const ctx = createMockContext();

      mockFetch.mockResolvedValueOnce(
        new Response(null, { status: 200 })
      );

      await email(message, env, ctx);

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const [, options] = mockFetch.mock.calls[0];
      expect(options.body).toBeInstanceOf(ArrayBuffer);
    });

    it('should log success when ingest returns 200 with accepted status', async () => {
      const { email } = await import('../src/index');

      const message = createMockMessage({
        from: 'sender@example.com',
        to: 'catchall@lite-mail.example.com',
      });
      const env = createMockEnv();
      const ctx = createMockContext();

      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ status: 'accepted' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      );

      await email(message, env, ctx);

      const waitUntilPromise = ctx.waitUntil.mock.calls[0][0];
      await waitUntilPromise;

      expect(mockConsoleLog).toHaveBeenCalledWith(
        'Email ingested successfully:',
        expect.objectContaining({
          from: 'sender@example.com',
          to: 'catchall@lite-mail.example.com',
        })
      );
    });

    it('should log duplicate when ingest returns 200 with duplicate status', async () => {
      const { email } = await import('../src/index');

      const message = createMockMessage({
        from: 'sender@example.com',
        to: 'catchall@lite-mail.example.com',
      });
      const env = createMockEnv();
      const ctx = createMockContext();

      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ status: 'duplicate' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      );

      await email(message, env, ctx);

      const waitUntilPromise = ctx.waitUntil.mock.calls[0][0];
      await waitUntilPromise;

      expect(mockConsoleLog).toHaveBeenCalledWith(
        'Duplicate email ignored:',
        expect.objectContaining({
          from: 'sender@example.com',
          to: 'catchall@lite-mail.example.com',
        })
      );
    });

    it('should handle 401 authentication failure', async () => {
      const { email } = await import('../src/index');

      const message = createMockMessage();
      const env = createMockEnv();
      const ctx = createMockContext();

      mockFetch.mockResolvedValueOnce(
        new Response(null, { status: 401, statusText: 'Unauthorized' })
      );

      await email(message, env, ctx);

      const waitUntilPromise = ctx.waitUntil.mock.calls[0][0];
      await waitUntilPromise;

      expect(mockConsoleError).toHaveBeenCalledWith(
        'Ingest authentication failed:',
        expect.objectContaining({
          from: 'sender@example.com',
          to: 'recipient@example.com',
          status: 401,
        })
      );
    });

    it('should handle 413 message too large', async () => {
      const { email } = await import('../src/index');

      const message = createMockMessage({
        rawSize: 50000000,
      });
      const env = createMockEnv();
      const ctx = createMockContext();

      mockFetch.mockResolvedValueOnce(
        new Response(null, { status: 413, statusText: 'Payload Too Large' })
      );

      await email(message, env, ctx);

      const waitUntilPromise = ctx.waitUntil.mock.calls[0][0];
      await waitUntilPromise;

      expect(mockConsoleError).toHaveBeenCalledWith(
        'Message too large:',
        expect.objectContaining({
          from: 'sender@example.com',
          to: 'recipient@example.com',
          rawSize: 50000000,
          status: 413,
        })
      );
    });

    it('should throw on 500 server error', async () => {
      const { email } = await import('../src/index');

      const message = createMockMessage();
      const env = createMockEnv();
      const ctx = createMockContext();

      mockFetch.mockResolvedValueOnce(
        new Response(null, { status: 500, statusText: 'Internal Server Error' })
      );

      await email(message, env, ctx);

      const waitUntilPromise = ctx.waitUntil.mock.calls[0][0];
      await expect(waitUntilPromise).rejects.toThrow('Ingest failed with status 500');

      expect(mockConsoleError).toHaveBeenCalledWith(
        'Ingest endpoint returned error:',
        expect.objectContaining({
          status: 500,
          statusText: 'Internal Server Error',
        })
      );
    });

    it('should handle network error', async () => {
      const { email } = await import('../src/index');

      const message = createMockMessage();
      const env = createMockEnv();
      const ctx = createMockContext();

      mockFetch.mockRejectedValueOnce(new Error('Network connection failed'));

      await email(message, env, ctx);

      const waitUntilPromise = ctx.waitUntil.mock.calls[0][0];
      await expect(waitUntilPromise).rejects.toThrow('Network connection failed');

      expect(mockConsoleError).toHaveBeenCalledWith(
        'Ingest request failed:',
        expect.any(Error)
      );
    });

    it('should handle timeout with AbortError', async () => {
      const { email } = await import('../src/index');

      const message = createMockMessage();
      const env = createMockEnv();
      const ctx = createMockContext();

      const abortController = new AbortController();
      mockFetch.mockImplementationOnce(() => {
        return new Promise((_, reject) => {
          setTimeout(() => reject(new DOMException('Aborted', 'AbortError')), 100);
        });
      });

      await email(message, env, ctx);

      const waitUntilPromise = ctx.waitUntil.mock.calls[0][0];
      await expect(waitUntilPromise).rejects.toThrow();

      expect(mockConsoleError).toHaveBeenCalledWith(
        'Ingest request timed out:',
        expect.objectContaining({
          url: 'https://mail.example.com/ingest',
        })
      );
    });
  });
});
