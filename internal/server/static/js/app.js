const state = {
  app: null,
  mailbox: null,
  sandbox: null,
  currentPage: 1,
  totalPages: 1,
  currentQuery: '',
  perPage: 20,
  mailboxTotal: 0,
  searchDebounceTimer: null,
  mailboxRequestToken: 0,
};

const ROUTES = {
  list: '#/',
  messagePrefix: '#/messages/',
};

function ensureAppRoot() {
  if (state.app) {
    return state.app;
  }

  const existingRoot = document.getElementById('app');
  if (existingRoot) {
    state.app = existingRoot;
    state.app.classList.add('lite-mail-app');
    return state.app;
  }

  const root = document.createElement('main');
  root.id = 'app';
  root.className = 'lite-mail-app';
  document.body.appendChild(root);
  state.app = root;
  return root;
}

async function loadSandboxHelpers() {
  if (state.sandbox) {
    return state.sandbox;
  }
  state.sandbox = await import('/static/js/sandbox.js');
  return state.sandbox;
}

async function apiFetch(url, options = {}) {
  const response = await fetch(url, {
    credentials: 'same-origin',
    headers: {
      Accept: 'application/json, text/plain;q=0.9, */*;q=0.8',
      ...(options.headers || {}),
    },
    ...options,
  });

  if (response.status === 401) {
    redirectToLogin();
    throw new Error('Unauthorized');
  }

  return response;
}

async function fetchJSON(url) {
  const response = await apiFetch(url);
  if (response.status === 404) {
    return null;
  }
  if (!response.ok) {
    throw new Error(`Request failed with status ${response.status}`);
  }
  return response.json();
}

function isUnauthorizedError(error) {
  if (!error || typeof error.message !== 'string') {
    return false;
  }

  return /unauthorized|authentication required/i.test(error.message);
}

function redirectToLogin() {
    if (window.location.pathname !== '/login') {
        history.pushState(null, '', '/login');
    }
    renderLogin();
}

function escapeRouteSegment(value) {
  return encodeURIComponent(String(value));
}

function navigateTo(route) {
  window.location.hash = route;
}

function clearSearchDebounce() {
  if (state.searchDebounceTimer) {
    window.clearTimeout(state.searchDebounceTimer);
    state.searchDebounceTimer = null;
  }
}

function clearApp() {
  const app = ensureAppRoot();
  app.replaceChildren();
  return app;
}

function renderLogin() {
  const app = clearApp();
  app.innerHTML = `
    <section class="login-shell">
      <div class="login-layout">
        <div class="login-hero">
          <p class="mail-eyebrow">Lite Mail</p>
          <h1 class="login-title">Private inbox access with a calmer reading surface.</h1>
          <p class="login-copy">Use your mailbox email address and access key to continue. Existing auth, routes, and state flow remain unchanged.</p>
          <div class="login-hero-meta" aria-hidden="true">
            <span class="message-chip login-hero-chip">Focused reading</span>
            <span class="message-chip login-hero-chip">Safe attachment review</span>
            <span class="message-chip login-hero-chip">Unchanged routing</span>
          </div>
        </div>
        <section class="panel login-panel" aria-labelledby="login-heading">
          <div class="login-panel-head">
            <p class="login-kicker">Sign in</p>
            <h2 id="login-heading" class="panel-title login-panel-title">Open your mailbox</h2>
            <p class="muted-copy">Authenticate to read captured mail, inspect attachments, and review raw source safely.</p>
          </div>
          <form id="login-form" class="login-form">
            <label class="form-field" for="psk">
              <span class="form-field-label">Access key</span>
              <input class="form-input" type="password" id="psk" name="psk" required autocomplete="current-password">
            </label>
            <label class="form-field" for="email">
              <span class="form-field-label">Email address</span>
              <input class="form-input" type="email" id="email" name="email" required autocomplete="email">
            </label>
            <div id="login-error" class="form-error" hidden role="alert"></div>
            <button type="submit" class="primary-button form-submit">Sign in</button>
          </form>
        </section>
      </div>
    </section>
  `;
  document.getElementById('login-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const psk = document.getElementById('psk').value;
    const email = document.getElementById('email').value;
    const errorEl = document.getElementById('login-error');
    errorEl.hidden = true;
    try {
      const res = await fetch('/api/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ psk, email }),
      });
      if (res.ok) {
        window.location.href = '/';
        return;
      }
      const data = await res.json().catch(() => ({}));
      errorEl.textContent = data.error || 'Invalid credentials';
      errorEl.hidden = false;
    } catch (err) {
      errorEl.textContent = 'Connection error';
      errorEl.hidden = false;
    }
  });
}

function renderShell(titleText, subtitleText) {
  const app = clearApp();
  const shell = document.createElement('section');
  shell.className = 'mail-shell';

  const masthead = document.createElement('header');
  masthead.className = 'mail-masthead';

  const eyebrow = document.createElement('p');
  eyebrow.className = 'mail-eyebrow';
  eyebrow.textContent = 'Lite Mail workspace';

  const title = document.createElement('h1');
  title.className = 'mail-title';
  title.textContent = titleText;

  const subtitle = document.createElement('p');
  subtitle.className = 'mail-subtitle';
  subtitle.textContent = subtitleText;

  masthead.append(eyebrow, title, subtitle);
  shell.appendChild(masthead);
  app.appendChild(shell);
  return shell;
}

function renderStatusCard(titleText, messageText, actions = []) {
  const card = document.createElement('section');
  card.className = 'panel status-panel';

  const title = document.createElement('h2');
  title.className = 'panel-title';
  title.textContent = titleText;

  const message = document.createElement('p');
  message.className = 'status-message';
  message.textContent = messageText;

  card.append(title, message);

  if (actions.length > 0) {
    const actionRow = document.createElement('div');
    actionRow.className = 'status-actions';
    actions.forEach((action) => actionRow.appendChild(action));
    card.appendChild(actionRow);
  }

  return card;
}

function createButton(text, className, onClick) {
  const button = document.createElement('button');
  button.type = 'button';
  button.className = className;
  button.textContent = text;
  button.addEventListener('click', onClick);
  return button;
}

function createLinkButton(text, href, className) {
  const link = document.createElement('a');
  link.className = className;
  link.href = href;
  link.textContent = text;
  return link;
}

function formatDate(value) {
  if (!value) {
    return 'Unknown date';
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(parsed);
}

function formatSize(bytes) {
  if (!Number.isFinite(bytes) || bytes < 0) {
    return 'Unknown size';
  }
  const units = ['B', 'KB', 'MB', 'GB'];
  let value = bytes;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }
  return `${value.toFixed(unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`;
}

function groupRecipients(recipients = []) {
  return recipients.reduce(
    (groups, recipient) => {
      const type = (recipient.type || 'to').toLowerCase();
      if (!groups[type]) {
        groups[type] = [];
      }
      groups[type].push(recipient.email || 'Unknown');
      return groups;
    },
    { to: [], cc: [], bcc: [] },
  );
}

function buildHeaderField(labelText, valueNode) {
  const wrap = document.createElement('div');
  wrap.className = 'message-field';

  const label = document.createElement('span');
  label.className = 'message-field-label';
  label.textContent = labelText;

  wrap.appendChild(label);
  wrap.appendChild(valueNode);
  return wrap;
}

function createFieldValue(text) {
  const value = document.createElement('span');
  value.className = 'message-field-value';
  value.textContent = text;
  return value;
}

function renderTextBody(text) {
  const pre = document.createElement('pre');
  pre.className = 'message-pre';
  pre.textContent = text || 'No plain text body available.';
  return pre;
}

async function renderHTMLBody(html) {
  const frameWrap = document.createElement('div');
  frameWrap.className = 'message-html-wrap';
  const { createSandboxedIframe } = await loadSandboxHelpers();
  createSandboxedIframe(html || '<p>No HTML body available.</p>', frameWrap);
  return frameWrap;
}

async function renderRawSource(messageId) {
  const pre = document.createElement('pre');
  pre.className = 'message-pre';
  pre.textContent = 'Loading raw source…';

  try {
    const response = await apiFetch(`/api/messages/${escapeRouteSegment(messageId)}/raw`, {
      headers: { Accept: 'message/rfc822, text/plain;q=0.9, */*;q=0.8' },
    });
    if (response.status === 404) {
      pre.textContent = 'Raw source not found.';
      return pre;
    }
    if (!response.ok) {
      throw new Error(`Request failed with status ${response.status}`);
    }
    pre.textContent = await response.text();
  } catch (error) {
    if (!isUnauthorizedError(error)) {
      pre.textContent = 'Unable to load raw source.';
    }
  }

  return pre;
}

function renderAttachments(attachments = [], messageId) {
  const section = document.createElement('section');
  section.className = 'panel attachment-panel';

  const header = document.createElement('div');
  header.className = 'section-heading';

  const title = document.createElement('h2');
  title.className = 'panel-title';
  title.textContent = `Attachments (${attachments.length})`;

  const titleMeta = document.createElement('p');
  titleMeta.className = 'section-meta';
  titleMeta.textContent = attachments.length > 0
    ? 'Download original files directly from the captured message.'
    : 'No downloadable files are attached to this message.';

  const headingGroup = document.createElement('div');
  headingGroup.className = 'section-title-group';
  headingGroup.append(title, titleMeta);

  const downloadOriginal = createLinkButton(
    'Download Original',
    `/api/messages/${escapeRouteSegment(messageId)}/raw`,
    'button-link secondary-button',
  );
  downloadOriginal.setAttribute('download', `message-${messageId}.eml`);

  header.append(headingGroup, downloadOriginal);
  section.appendChild(header);

  if (attachments.length === 0) {
    const empty = document.createElement('p');
    empty.className = 'muted-copy';
    empty.textContent = 'No attachments were included with this message.';
    section.appendChild(empty);
    return section;
  }

  const list = document.createElement('ul');
  list.className = 'attachment-list';

  attachments.forEach((attachment, index) => {
    const item = document.createElement('li');
    item.className = 'attachment-item';

    const badge = document.createElement('span');
    badge.className = 'attachment-badge';
    badge.textContent = '↓';
    badge.setAttribute('aria-hidden', 'true');

    const info = document.createElement('div');
    info.className = 'attachment-meta';

    const name = document.createElement('strong');
    name.className = 'attachment-name';
    name.textContent = attachment.original_filename || `Attachment ${index + 1}`;

    const detail = document.createElement('span');
    detail.className = 'attachment-detail';
    detail.textContent = `${attachment.mime_type || 'application/octet-stream'} · ${formatSize(attachment.size_bytes)}`;

    info.append(name, detail);

    const link = createLinkButton(
      'Download',
      `/api/messages/${escapeRouteSegment(messageId)}/attachments/${index}`,
      'button-link tertiary-button',
    );
    link.setAttribute('download', attachment.original_filename || `attachment-${index + 1}`);

    item.append(badge, info, link);
    list.appendChild(item);
  });

  section.appendChild(list);
  return section;
}

function activateTab(tabs, panels, activeKey) {
  tabs.forEach((tab) => {
    const isActive = tab.dataset.tabKey === activeKey;
    tab.classList.toggle('is-active', isActive);
    tab.setAttribute('aria-selected', String(isActive));
    tab.tabIndex = isActive ? 0 : -1;
  });

  panels.forEach((panel) => {
    const isActive = panel.dataset.panelKey === activeKey;
    panel.hidden = !isActive;
    panel.classList.toggle('is-active', isActive);
  });
}

async function buildTabPanels(message) {
  const tabDefinitions = [];

  if (message.html_body) {
    tabDefinitions.push({ key: 'html', label: 'HTML', render: () => renderHTMLBody(message.html_body) });
  }

  tabDefinitions.push({ key: 'text', label: 'Text', render: () => Promise.resolve(renderTextBody(message.text_body)) });
  tabDefinitions.push({ key: 'raw', label: 'Raw Source', render: () => renderRawSource(message.id) });

  const tabs = document.createElement('div');
  tabs.className = 'message-tabs';
  tabs.setAttribute('role', 'tablist');
  tabs.setAttribute('aria-label', 'Message body views');

  const content = document.createElement('div');
  content.className = 'panel message-content';

  const tabButtons = [];
  const panels = [];

  for (const [index, definition] of tabDefinitions.entries()) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'message-tab';
    button.dataset.tabKey = definition.key;
    button.textContent = definition.label;
    button.setAttribute('role', 'tab');
    button.id = `tab-${definition.key}`;

    const panel = document.createElement('section');
    panel.className = 'message-panel';
    panel.dataset.panelKey = definition.key;
    panel.setAttribute('role', 'tabpanel');
    panel.setAttribute('aria-labelledby', button.id);
    panel.hidden = index !== 0;
    if (index === 0) {
      button.classList.add('is-active');
      button.setAttribute('aria-selected', 'true');
    } else {
      button.setAttribute('aria-selected', 'false');
      button.tabIndex = -1;
    }

    const node = await definition.render();
    panel.appendChild(node);

    button.addEventListener('click', () => activateTab(tabButtons, panels, definition.key));

    tabButtons.push(button);
    panels.push(panel);
    tabs.appendChild(button);
    content.appendChild(panel);
  }

  return { tabs, content };
}

async function renderMessageView(message) {
  const shell = renderShell('Message detail', 'Inspect the message safely before downloading anything.');

  const toolbar = document.createElement('div');
  toolbar.className = 'mail-toolbar';
  toolbar.appendChild(createButton('← Back to mailbox', 'primary-button', () => navigateTo(ROUTES.list)));
  shell.appendChild(toolbar);

  const hero = document.createElement('section');
  hero.className = 'panel message-hero';

  const heroTop = document.createElement('div');
  heroTop.className = 'message-hero-top';

  const titleGroup = document.createElement('div');
  titleGroup.className = 'message-title-group';

  const messageKicker = document.createElement('p');
  messageKicker.className = 'message-kicker';
  messageKicker.textContent = 'Captured message';

  const subject = document.createElement('h2');
  subject.className = 'message-subject';
  subject.textContent = message.subject || '(No subject)';

  const stamp = document.createElement('p');
  stamp.className = 'message-date';
  stamp.textContent = formatDate(message.message_date || message.received_at);

  const messageSummary = document.createElement('div');
  messageSummary.className = 'message-summary';

  const senderSummary = document.createElement('span');
  senderSummary.className = 'message-chip message-summary-chip';
  senderSummary.textContent = message.sender || 'Unknown sender';

  const attachmentSummary = document.createElement('span');
  attachmentSummary.className = 'message-chip message-summary-chip';
  attachmentSummary.textContent = `${message.attachments.length} attachment${message.attachments.length === 1 ? '' : 's'}`;

  messageSummary.append(senderSummary, attachmentSummary);
  titleGroup.append(messageKicker, subject, stamp, messageSummary);
  heroTop.appendChild(titleGroup);
  hero.appendChild(heroTop);

  const fields = document.createElement('div');
  fields.className = 'message-fields';

  fields.appendChild(buildHeaderField('From', createFieldValue(message.sender || 'Unknown sender')));

  const recipientGroups = groupRecipients(message.recipients);
  fields.appendChild(buildHeaderField('To', createFieldValue(recipientGroups.to.join(', ') || 'No visible recipients')));
  if (recipientGroups.cc.length > 0) {
    fields.appendChild(buildHeaderField('Cc', createFieldValue(recipientGroups.cc.join(', '))));
  }
  if (recipientGroups.bcc.length > 0) {
    fields.appendChild(buildHeaderField('Bcc', createFieldValue(recipientGroups.bcc.join(', '))));
  }
  fields.appendChild(buildHeaderField('Received', createFieldValue(formatDate(message.received_at))));

  hero.appendChild(fields);
  shell.appendChild(hero);

  if (message.parser_status && message.parser_status !== 'success') {
    const banner = document.createElement('div');
    banner.className = message.parser_status === 'failed' ? 'parser-error-banner' : 'parser-warning-banner';
    if (message.parser_status === 'partial') {
      banner.textContent = '⚠ This message was partially parsed. Some content may be missing.';
    } else if (message.parser_status === 'failed') {
      banner.textContent = '⚠ This message could not be fully parsed. Raw MIME content is available.';
    }
    shell.appendChild(banner);
  }

  const { tabs, content } = await buildTabPanels(message);
  shell.append(tabs, content, renderAttachments(message.attachments, message.id));
}

function renderMessageNotFound() {
  const shell = renderShell('Message not found', 'The message may have been removed or you may not have access.');
  shell.appendChild(
    renderStatusCard('Nothing to show', 'We could not find that message.', [
      createButton('Back to mailbox', 'primary-button', () => navigateTo(ROUTES.list)),
    ]),
  );
}

function renderErrorState(titleText, messageText) {
  const shell = renderShell(titleText, 'Please try again in a moment.');
  shell.appendChild(
    renderStatusCard(titleText, messageText, [
      createButton('Back to mailbox', 'primary-button', () => navigateTo(ROUTES.list)),
    ]),
  );
}

function renderMailboxPagination() {
  const pagination = document.createElement('div');
  pagination.className = 'message-pagination';

  const previousButton = createButton('Previous', 'secondary-button message-pagination-button', () => {
    void loadMailboxPage(state.currentPage - 1, state.currentQuery || '');
  });
  previousButton.disabled = state.currentPage <= 1;

  const pageIndicator = document.createElement('p');
  pageIndicator.className = 'message-pagination-indicator';
  pageIndicator.textContent = `Page ${state.currentPage} of ${state.totalPages}`;

  const nextButton = createButton('Next', 'secondary-button message-pagination-button', () => {
    void loadMailboxPage(state.currentPage + 1, state.currentQuery || '');
  });
  nextButton.disabled = state.currentPage >= state.totalPages;

  pagination.append(previousButton, pageIndicator, nextButton);
  return pagination;
}

function syncMailboxState(payload, query) {
  const total = Number(payload?.total) || 0;
  const perPage = Number(payload?.per_page) || state.perPage || 20;
  const totalPages = total > 0 ? Math.ceil(total / perPage) : 1;

  state.mailbox = payload?.messages || [];
  state.mailboxTotal = total;
  state.perPage = perPage;
  state.currentPage = Math.min(Math.max(Number(payload?.page) || 1, 1), totalPages);
  state.totalPages = totalPages;
  state.currentQuery = typeof query === 'string' ? query : state.currentQuery || '';
}

async function loadMailboxPage(page = 1, query = '', options = {}) {
  const { showLoading = false } = options;
  const nextQuery = typeof query === 'string' ? query : '';
  const nextPage = Math.max(1, Number(page) || 1);
  const requestToken = ++state.mailboxRequestToken;

  if (showLoading) {
    renderShell('Inbox', 'Loading your messages…');
  }

  try {
    const payload = await window.API.listMessages(nextPage, state.perPage || 20, nextQuery);
    if (requestToken !== state.mailboxRequestToken) {
      return;
    }

    syncMailboxState(payload, nextQuery);
    renderMessageList();
  } catch (error) {
    if (requestToken !== state.mailboxRequestToken) {
      return;
    }
    if (isUnauthorizedError(error)) {
      redirectToLogin();
      return;
    }
    renderErrorState('Mailbox unavailable', 'We could not load the mailbox right now.');
  }
}

function renderMessageList(messages = state.mailbox || []) {
  const shell = renderShell('Inbox', 'Browse, search, and inspect captured mail with a clearer reading hierarchy.');

  const listPanel = document.createElement('section');
  listPanel.className = 'panel message-list-panel';

  const listHeader = document.createElement('div');
  listHeader.className = 'message-list-header';

  const title = document.createElement('h2');
  title.className = 'panel-title';
  title.textContent = 'Messages';

  const summary = document.createElement('p');
  summary.className = 'section-meta';
  summary.textContent = `${state.mailboxTotal} total message${state.mailboxTotal === 1 ? '' : 's'} across ${state.totalPages} page${state.totalPages === 1 ? '' : 's'}.`;

  const searchBlock = document.createElement('div');
  searchBlock.className = 'message-search';

  const searchLabel = document.createElement('label');
  searchLabel.className = 'message-search-label';
  searchLabel.setAttribute('for', 'message-search-input');
  searchLabel.textContent = 'Search mail';

  const searchField = document.createElement('div');
  searchField.className = 'message-search-field';

  const searchInput = document.createElement('input');
  searchInput.id = 'message-search-input';
  searchInput.className = 'message-search-input';
  searchInput.type = 'search';
  searchInput.placeholder = 'Search messages…';
  searchInput.value = state.currentQuery || '';
  searchInput.setAttribute('aria-label', 'Search messages');
  searchInput.addEventListener('input', (event) => {
    const nextQuery = event.currentTarget.value;
    state.currentQuery = nextQuery;
    clearSearchButton.disabled = !nextQuery.trim();
    clearSearchDebounce();
    state.searchDebounceTimer = window.setTimeout(() => {
      state.searchDebounceTimer = null;
      void loadMailboxPage(1, nextQuery);
    }, 300);
  });

  const clearSearchButton = createButton('×', 'message-search-clear', () => {
    if (!state.currentQuery) {
      searchInput.focus();
      return;
    }
    clearSearchDebounce();
    state.currentQuery = '';
    searchInput.value = '';
    searchInput.focus();
    void loadMailboxPage(1, '');
  });
  clearSearchButton.setAttribute('aria-label', 'Clear search');
  clearSearchButton.disabled = !(state.currentQuery || '').trim();

  searchField.append(searchInput, clearSearchButton);
  searchBlock.append(searchLabel, searchField);
  listHeader.append(title, summary);
  listPanel.append(listHeader, searchBlock);

  if (messages.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'message-empty-state';

    const emptyTitle = document.createElement('p');
    emptyTitle.className = 'message-empty-title';

    const emptyMessage = document.createElement('p');
    emptyMessage.className = 'muted-copy';

    if ((state.currentQuery || '').trim()) {
      emptyTitle.textContent = 'No messages found';
      emptyMessage.append('No results for ');
      const queryHighlight = document.createElement('strong');
      queryHighlight.className = 'message-search-query';
      queryHighlight.textContent = `“${state.currentQuery.trim()}”`;
      emptyMessage.append(queryHighlight, '. Try a different phrase.');
    } else {
      emptyTitle.textContent = 'No messages yet';
      emptyMessage.textContent = 'Incoming mail will appear here as soon as it arrives.';
    }

    empty.append(emptyTitle, emptyMessage);
    listPanel.appendChild(empty);
    listPanel.appendChild(renderMailboxPagination());
    shell.appendChild(listPanel);
    return;
  }

  const list = document.createElement('div');
  list.className = 'message-list';

  messages.forEach((message) => {
    const item = document.createElement('button');
    item.type = 'button';
    item.className = 'message-list-item';
    item.addEventListener('click', () => navigateTo(`${ROUTES.messagePrefix}${escapeRouteSegment(message.id)}`));

    const top = document.createElement('div');
    top.className = 'message-list-top';

    const subject = document.createElement('strong');
    subject.className = 'message-list-subject';
    subject.textContent = message.subject || '(No subject)';

    const date = document.createElement('span');
    date.className = 'message-list-date';
    date.textContent = formatDate(message.message_date || message.received_at);

    top.append(subject, date);

     const meta = document.createElement('p');
     meta.className = 'message-list-meta';
     meta.textContent = `${message.sender || 'Unknown sender'} · ${message.text_body ? message.text_body.slice(0, 120).replace(/\s+/g, ' ') : 'Open to inspect the message body.'}`;

     const metaRow = document.createElement('div');
     metaRow.className = 'message-list-row';

     const senderChip = document.createElement('span');
     senderChip.className = 'message-chip';
     senderChip.textContent = message.sender || 'Unknown sender';

     const preview = document.createElement('p');
     preview.className = 'message-preview';
     preview.textContent = message.text_body ? message.text_body.slice(0, 160).replace(/\s+/g, ' ') : 'Open to inspect the message body.';

    metaRow.append(senderChip);
    item.append(top, metaRow, preview, meta);
    list.appendChild(item);
  });

  listPanel.appendChild(list);
  listPanel.appendChild(renderMailboxPagination());
  shell.appendChild(listPanel);
}

async function showMailboxList() {
  clearSearchDebounce();
  const nextPage = state.currentPage || 1;
  const nextQuery = state.currentQuery || '';
  await loadMailboxPage(nextPage, nextQuery, { showLoading: true });
}

async function showMessageView(messageId) {
  renderShell('Message detail', 'Loading message…');
  try {
    const message = await fetchJSON(`/api/messages/${escapeRouteSegment(messageId)}`);
    if (!message) {
      renderMessageNotFound();
      return;
    }
    await renderMessageView(message);
  } catch (error) {
    if (isUnauthorizedError(error)) {
      redirectToLogin();
      return;
    }
    renderErrorState('Message not found', 'We could not load this message.');
  }
}

function parseRoute(hashValue) {
  const hash = hashValue || ROUTES.list;
  if (hash === '' || hash === '#') {
    return { type: 'list' };
  }
  if (hash === ROUTES.list) {
    return { type: 'list' };
  }
  if (hash.startsWith(ROUTES.messagePrefix)) {
    const messageId = decodeURIComponent(hash.slice(ROUTES.messagePrefix.length)).trim();
    if (/^\d+$/.test(messageId)) {
      return { type: 'message', messageId };
    }
  }
  return { type: 'not-found' };
}

async function handleNavigation() {
  const route = parseRoute(window.location.hash);
  if (route.type === 'message') {
    await showMessageView(route.messageId);
    return;
  }
  if (route.type === 'list') {
    await showMailboxList();
    return;
  }
  renderMessageNotFound();
}

function boot() {
  ensureAppRoot();
  window.addEventListener('lite-mail:unauthorized', redirectToLogin);
  window.addEventListener('hashchange', handleNavigation);
  void handleNavigation();
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', boot, { once: true });
} else {
  boot();
}

window.liteMailApp = {
  handleNavigation,
  renderMessageView,
  showMessageView,
  renderTextBody,
  renderRawSource,
  renderAttachments,
};
