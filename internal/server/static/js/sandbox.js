const SANDBOX_CSP = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:;";

function wrapHTMLDocument(htmlContent) {
  const safeHTML = typeof htmlContent === 'string' ? htmlContent : '';
  return `<!DOCTYPE html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${SANDBOX_CSP}"><meta name="viewport" content="width=device-width, initial-scale=1"></head><body>${safeHTML}</body></html>`;
}

export function createSandboxedIframe(htmlContent, container) {
  if (!(container instanceof HTMLElement)) {
    throw new Error('Sandbox container is required');
  }

  destroySandboxedIframe(container);

  const iframe = document.createElement('iframe');
  iframe.className = 'message-html-frame';
  iframe.setAttribute('sandbox', 'allow-same-origin');
  iframe.setAttribute('referrerpolicy', 'no-referrer');
  iframe.setAttribute('title', 'HTML email content');

  const content = wrapHTMLDocument(htmlContent);

  iframe.addEventListener('load', () => {
    try {
      const doc = iframe.contentDocument;
      if (!doc) {
        return;
      }
      const root = doc.documentElement;
      const body = doc.body;
      const height = Math.max(
        root?.scrollHeight || 0,
        body?.scrollHeight || 0,
        root?.offsetHeight || 0,
        body?.offsetHeight || 0,
        320,
      );
      iframe.style.height = `${height}px`;
    } catch (_error) {
      iframe.style.height = '28rem';
    }
  });

  iframe.addEventListener('error', () => {
    container.textContent = 'Unable to render HTML email safely.';
  });

  try {
    iframe.srcdoc = content;
  } catch (_error) {
    container.textContent = 'Unable to render HTML email safely.';
    return null;
  }

  container.appendChild(iframe);
  return iframe;
}

export function destroySandboxedIframe(container) {
  if (!(container instanceof HTMLElement)) {
    return;
  }
  const frame = container.querySelector('iframe');
  if (frame) {
    frame.remove();
  }
}
