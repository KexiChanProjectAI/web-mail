import type { ForwardableEmailMessage } from '@cloudflare/workers-types';

export interface Env {
  INGEST_URL: string;
  WORKER_INGEST_PSK: string;
}

interface WorkerContext {
  waitUntil(promise: Promise<unknown>): void;
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
  ctx: WorkerContext
): Promise<void> {
  const from = message.from;
  const to = message.to;
  const rawSize = message.rawSize;

  let rawMIME: Uint8Array;
  try {
    rawMIME = await collectStream(message.raw);
  } catch (err) {
    console.error('Failed to read email raw stream:', err);
    message.setReject('Failed to read email content');
    return;
  }

  const ingestUrl = env.INGEST_URL;
  const ingestPSK = env.WORKER_INGEST_PSK;

  if (!ingestUrl || !ingestPSK) {
    console.error('Missing required environment configuration:', {
      hasIngestUrl: !!ingestUrl,
      hasIngestPSK: !!ingestPSK,
    });
    message.setReject('Worker misconfigured: missing ingest URL or PSK');
    return;
  }

  ctx.waitUntil(
    postToIngest(ingestUrl, ingestPSK, rawMIME.buffer, { from, to, rawSize })
  );
}

async function postToIngest(
  url: string,
  psk: string,
  body: ArrayBuffer,
  meta: { from: string; to: string; rawSize: number }
): Promise<void> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 30000);

  try {
    const response = await fetch(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'message/rfc822',
        'X-Lite-Mail-Ingest-PSK': psk,
      },
      body,
      signal: controller.signal,
    });

    if (response.status === 401) {
      console.error('Ingest authentication failed:', {
        from: meta.from,
        to: meta.to,
        status: 401,
      });
      return;
    }

    if (response.status === 413) {
      console.error('Message too large:', {
        from: meta.from,
        to: meta.to,
        rawSize: meta.rawSize,
        status: 413,
      });
      return;
    }

    if (response.ok) {
      let responseText = '';
      try {
        responseText = await response.text();
        const data = JSON.parse(responseText);

        if (data.status === 'accepted') {
          console.log('Email ingested successfully:', {
            from: meta.from,
            to: meta.to,
            rawSize: meta.rawSize,
          });
          return;
        }

        if (data.status === 'duplicate') {
          console.log('Duplicate email ignored:', {
            from: meta.from,
            to: meta.to,
            rawSize: meta.rawSize,
          });
          return;
        }
      } catch {
        console.log('Email ingested successfully:', {
          from: meta.from,
          to: meta.to,
          rawSize: meta.rawSize,
          note: 'Could not parse response',
        });
        return;
      }
    }

    console.error('Ingest endpoint returned error:', {
      status: response.status,
      statusText: response.statusText,
      from: meta.from,
      to: meta.to,
      rawSize: meta.rawSize,
    });
    throw new Error(`Ingest failed with status ${response.status}`);
  } catch (err) {
    if (err instanceof Error && err.name === 'AbortError') {
      console.error('Ingest request timed out:', { url, from: meta.from, to: meta.to });
    } else {
      console.error('Ingest request failed:', err);
    }
    throw err;
  } finally {
    clearTimeout(timeout);
  }
}

export { email };
export default {
  email,
};
