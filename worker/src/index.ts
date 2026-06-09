import type { ExecutionContext, ForwardableEmailMessage } from '@cloudflare/workers-types';

export interface Env {
  INGEST_URL?: string;
  WORKER_INGEST_PSK?: string;
}

class RejectableDeliveryError extends Error {
  reason: string;

  constructor(message: string, reason: string) {
    super(message);
    this.name = 'RejectableDeliveryError';
    this.reason = reason;
  }
}

interface IngestMeta {
  from: string;
  to: string;
  rawSize: number;
  messageId: string | null;
  subject: string | null;
  ingestTarget?: string;
}

function buildIngestMeta(
  message: Pick<ForwardableEmailMessage, 'from' | 'to' | 'rawSize' | 'headers'>,
  ingestUrl?: string,
): IngestMeta {
  return {
    from: message.from,
    to: message.to,
    rawSize: message.rawSize,
    messageId: message.headers.get('message-id'),
    subject: message.headers.get('subject'),
    ingestTarget: ingestUrl,
  };
}

async function collectStream(stream: ReadableStream): Promise<Uint8Array> {
  const reader = stream.getReader();
  const chunks: Uint8Array[] = [];

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    chunks.push(value);
  }
  reader.releaseLock();

  const totalLength = chunks.reduce((acc, chunk) => acc + chunk.length, 0);
  const result = new Uint8Array(totalLength);
  let offset = 0;
  for (const chunk of chunks) {
    result.set(chunk, offset);
    offset += chunk.length;
  }
  return result;
}

async function email(
  message: ForwardableEmailMessage,
  env: Env,
  ctx: ExecutionContext
): Promise<void> {
  const ingestUrl = env.INGEST_URL;
  const ingestPSK = env.WORKER_INGEST_PSK;
  const meta = buildIngestMeta(message, ingestUrl);

  console.log('Email event received:', meta);

  let rawMIME: Uint8Array;
  try {
    rawMIME = await collectStream(message.raw);
    console.log('Email raw stream collected:', {
      ...meta,
      collectedBytes: rawMIME.byteLength,
    });
  } catch (err) {
    console.error('Failed to read email raw stream:', err);
    message.setReject('Failed to read email content');
    return;
  }

  if (!ingestUrl || !ingestPSK) {
    console.error('Missing required environment configuration:', {
      ...meta,
      hasIngestUrl: !!ingestUrl,
      hasIngestPSK: !!ingestPSK,
    });
    message.setReject('Worker misconfigured: missing ingest URL or PSK');
    return;
  }

  console.log('Delivering email to ingest endpoint:', meta);

  try {
    await postToIngest(ingestUrl, ingestPSK, rawMIME.buffer as ArrayBuffer, meta);
  } catch (err) {
    const reason = err instanceof RejectableDeliveryError
      ? err.reason
      : 'Mail delivery failed: unexpected error';
    console.error('Email delivery failed:', { ...meta, reason });
    message.setReject(reason);
    return;
  }
}

async function attemptIngest(
  url: string,
  psk: string,
  body: ArrayBuffer,
  meta: IngestMeta,
  signal: AbortSignal
): Promise<number | void> {
  console.log('Posting email to ingest endpoint:', {
    ...meta,
    bodyBytes: body.byteLength,
  });

  const response = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'message/rfc822',
      'X-Lite-Mail-Ingest-PSK': psk,
    },
    body,
    signal,
  });

  if (response.status === 401) {
    console.error('Ingest authentication failed:', {
      ...meta,
      status: 401,
    });
    throw new RejectableDeliveryError(
      'Ingest authentication failed',
      'Mail delivery failed: ingest authentication rejected the message',
    );
  }

  if (response.status === 413) {
    console.error('Message too large:', {
      ...meta,
      status: 413,
    });
    throw new RejectableDeliveryError(
      'Message too large',
      'Mail delivery failed: message exceeds ingest size limits',
    );
  }

  if (response.ok) {
    let responseText = '';
    try {
      responseText = await response.text();
      const data = JSON.parse(responseText);

      if (data.status === 'accepted') {
        console.log('Email ingested successfully:', { ...meta });
        return;
      }

      if (data.status === 'duplicate') {
        console.log('Duplicate email ignored:', { ...meta });
        return;
      }
    } catch {
      console.warn('Email ingested (unparseable response):', {
        ...meta,
        note: 'Could not parse response',
      });
      return;
    }
  }

  // Non-ok, non-401/413 status — return status so caller can decide on retry
  return response.status;
}

async function postToIngest(
  url: string,
  psk: string,
  body: ArrayBuffer,
  meta: IngestMeta
): Promise<void> {
  const controller = new AbortController();
  // Hardcoded 30s timeout; there is no runtime config for this.
  // 30s is the Cloudflare Worker fetch() subrequest limit.
  const timeout = setTimeout(() => controller.abort(), 30000);

  try {
    let result = await attemptIngest(url, psk, body, meta, controller.signal);

    // If result is undefined, the request was accepted (success)
    if (result === undefined) {
      return;
    }

    // If result is a status number and it's 5xx, retry once
    if (result >= 500 && result < 600) {
      console.warn('Ingest server returned 5xx, retrying once:', {
        ...meta,
        status: result,
      });

      // New AbortController for retry
      const retryController = new AbortController();
      const retryTimeout = setTimeout(() => retryController.abort(), 30000);

      try {
        const retryResult = await attemptIngest(url, psk, body, meta, retryController.signal);
        if (retryResult === undefined) {
          return; // Retry succeeded
        }
        // Retry also failed — fall through to error below
        result = retryResult;
      } finally {
        clearTimeout(retryTimeout);
      }
    }

    // If we reach here, both attempts failed or non-5xx error
    throw new RejectableDeliveryError(
      'Ingest server error',
      `Mail delivery failed: ingest server returned status ${result}`
    );
  } catch (err) {
    // Re-throw RejectableDeliveryError directly — already has proper reason
    if (err instanceof RejectableDeliveryError) {
      throw err;
    }
    if (err instanceof Error && err.name === 'AbortError') {
      console.error('Ingest request timed out:', { ...meta, url });
      throw new RejectableDeliveryError(
        'Ingest timeout',
        'Mail delivery failed: ingest server did not respond in time'
      );
    }
    console.error('Ingest request context:', meta);
    console.error('Ingest request failed:', err);
    const message = err instanceof Error ? err.message : 'Unknown error';
    throw new RejectableDeliveryError(
      'Ingest request failed',
      `Mail delivery failed: ${message}`
    );
  } finally {
    clearTimeout(timeout);
  }
}

export { email };
export default { email: email };
