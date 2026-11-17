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
  logoutPending: false,
  logoutError: '',
  me: null,
  providers: null,
  selectedMailbox: '',
  mailboxError: '',
  admin: null,
  audit: null,
};

const ROUTES = {
  list: '#/',
  messagePrefix: '#/messages/',
  admin: '#/admin',
  audit: '#/admin/audit',
};

const LOGIN_ERRORS = {
  oauth_denied: 'Google sign-in was cancelled.',
  oauth_failed: 'Google sign-in did not complete. Please try again.',
  email_unverified: 'Your Google account email is not verified.',
  domain_not_allowed: 'This Google account is not from an allowed domain.',
};

const PROVIDER_LABELS = {
  google: 'Google',
  cf_access: 'Cloudflare Access',
  admin_psk: 'Emergency key',
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
  resetMailboxState();
  state.me = null;
  state.admin = null;
  state.audit = null;
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

function resetMailboxState() {
  clearSearchDebounce();
  state.mailbox = null;
  state.currentPage = 1;
  state.totalPages = 1;
  state.currentQuery = '';
  state.mailboxTotal = 0;
  state.mailboxRequestToken += 1;
  state.logoutPending = false;
  state.logoutError = '';
}

async function handleMailboxLogout() {
  if (state.logoutPending) {
    return;
  }

  state.logoutPending = true;
  state.logoutError = '';
  const provider = state.me?.provider;

  try {
    const result = await window.API.logout();
    if (provider === 'cf_access' && result?.access_logout) {
      // The app cannot end a Cloudflare Access session itself; hand over to Access.
      window.location.assign(result.access_logout);
      return;
    }
    redirectToLogin();
  } catch (error) {
    if (isUnauthorizedError(error)) {
      return;
    }

    state.logoutPending = false;
    state.logoutError = 'We could not sign you out right now. Please try again.';
    void handleNavigation();
  }
}

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) {
    node.className = className;
  }
  if (text !== undefined) {
    node.textContent = text;
  }
  return node;
}

async function renderLogin() {
  const app = clearApp();
  if (!state.providers) {
    try {
      state.providers = await window.API.providers();
    } catch (error) {
      state.providers = { google: false, cf_access: false, admin_psk: false };
    }
  }
  const providers = state.providers;
  const errorCode = new URLSearchParams(window.location.search).get('error');

  const shell = el('section', 'login-shell');
  const layout = el('div', 'login-layout');

  const hero = el('div', 'login-hero');
  hero.append(
    el('p', 'mail-eyebrow', 'Lite Mail'),
    el('h1', 'login-title', 'Private inbox access with a calmer reading surface.'),
    el('p', 'login-copy', 'Sign in with your organisation account. An administrator decides which mailboxes each account can read.'),
  );

  const panel = el('section', 'panel login-panel');
  panel.setAttribute('aria-labelledby', 'login-heading');
  const head = el('div', 'login-panel-head');
  const heading = el('h2', 'panel-title login-panel-title', 'Sign in');
  heading.id = 'login-heading';
  head.append(heading);
  panel.appendChild(head);

  if (errorCode) {
    const banner = el('div', 'form-error', LOGIN_ERRORS[errorCode] || 'Sign-in failed. Please try again.');
    banner.setAttribute('role', 'alert');
    panel.appendChild(banner);
  }

  const methods = el('div', 'login-methods');
  if (providers.google) {
    const google = createLinkButton('Sign in with Google', '/auth/google/start', 'primary-button login-provider-button');
    methods.appendChild(google);
  }
  if (providers.cf_access) {
    if (providers.google) {
      methods.appendChild(el('div', 'login-divider', 'or'));
    }
    const access = createLinkButton('Continue with Cloudflare Access', '/', providers.google ? 'secondary-button login-provider-button' : 'primary-button login-provider-button');
    const note = el('p', 'muted-copy login-access-note',
      'Cloudflare Zero Trust signs you in with your organisation account and brings you back here.');
    methods.append(access, note);
  }
  if (!providers.google && !providers.cf_access && !providers.admin_psk) {
    methods.appendChild(el('p', 'muted-copy', 'No sign-in method is configured. Contact the administrator.'));
  }
  panel.appendChild(methods);

  if (providers.admin_psk) {
    const details = el('details', 'login-emergency');
    details.appendChild(el('summary', 'login-emergency-summary', 'Emergency admin access'));
    const form = el('form', 'login-form');
    form.id = 'login-form';
    const label = el('label', 'form-field');
    label.setAttribute('for', 'psk');
    label.appendChild(el('span', 'form-field-label', 'Admin key'));
    const input = el('input', 'form-input');
    Object.assign(input, { type: 'password', id: 'psk', name: 'psk', required: true, autocomplete: 'current-password' });
    label.appendChild(input);
    const formError = el('div', 'form-error');
    formError.hidden = true;
    formError.setAttribute('role', 'alert');
    const submit = el('button', 'secondary-button form-submit', 'Sign in with admin key');
    submit.type = 'submit';
    form.append(label, formError, submit);
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      formError.hidden = true;
      submit.disabled = true;
      try {
        await window.API.login(input.value);
        window.location.assign('/');
      } catch (err) {
        formError.textContent = /unauthorized|invalid/i.test(err.message) ? 'That key was not accepted.' : 'Connection error';
        formError.hidden = false;
        submit.disabled = false;
      }
    });
    details.appendChild(form);
    details.addEventListener('toggle', () => {
      if (details.open) {
        input.focus();
      }
    });
    panel.appendChild(details);
  }

  layout.append(hero, panel);
  shell.appendChild(layout);
  app.appendChild(shell);
}

function renderAccountBar() {
  const me = state.me;
  const bar = el('div', 'account-bar');
  if (!me) {
    return bar;
  }
  const who = el('div', 'account-identity');
  who.append(el('span', 'account-email', me.email));
  who.append(el('span', 'message-chip account-provider', PROVIDER_LABELS[me.provider] || me.provider));
  if (me.is_admin) {
    who.append(el('span', 'message-chip account-admin-chip', 'Admin'));
  }
  const actions = el('div', 'account-actions');
  const onAdmin = window.location.hash === ROUTES.admin || window.location.hash === ROUTES.audit;
  if (me.is_admin) {
    actions.appendChild(onAdmin
      ? createButton('Inbox', 'tertiary-button', () => navigateTo(ROUTES.list))
      : createButton('Manage access', 'tertiary-button', () => navigateTo(ROUTES.admin)));
  }
  const signOut = createButton(state.logoutPending ? 'Signing out…' : 'Sign out', 'tertiary-button', () => {
    void handleMailboxLogout();
  });
  signOut.disabled = state.logoutPending;
  actions.appendChild(signOut);
  bar.append(who, actions);
  if (state.logoutError) {
    const error = el('div', 'form-error account-error', state.logoutError);
    error.setAttribute('role', 'alert');
    bar.appendChild(error);
  }
  return bar;
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

  masthead.append(renderAccountBar(), eyebrow, title, subtitle);
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
  const attachments = Array.isArray(message.attachments) ? message.attachments : [];
  const recipients = Array.isArray(message.recipients) ? message.recipients : [];
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
  attachmentSummary.textContent = `${attachments.length} attachment${attachments.length === 1 ? '' : 's'}`;

  messageSummary.append(senderSummary, attachmentSummary);
  titleGroup.append(messageKicker, subject, stamp, messageSummary);
  heroTop.appendChild(titleGroup);
  hero.appendChild(heroTop);

  const fields = document.createElement('div');
  fields.className = 'message-fields';

  fields.appendChild(buildHeaderField('From', createFieldValue(message.sender || 'Unknown sender')));

  const recipientGroups = groupRecipients(recipients);
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
  shell.append(tabs, content, renderAttachments(attachments, message.id));
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
      createButton('Back to mailbox', 'primary-button', goToInbox),
    ]),
  );
}

function goToInbox() {
  if (window.location.hash === ROUTES.list) {
    void handleNavigation();
    return;
  }
  navigateTo(ROUTES.list);
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
    const payload = await window.API.listMessages(nextPage, state.perPage || 20, nextQuery, state.selectedMailbox);
    if (requestToken !== state.mailboxRequestToken) {
      return;
    }

    syncMailboxState(payload, nextQuery);
    state.mailboxError = '';
    renderMessageList();
  } catch (error) {
    if (requestToken !== state.mailboxRequestToken) {
      return;
    }
    if (isUnauthorizedError(error)) {
      redirectToLogin();
      return;
    }
    if (state.selectedMailbox && /not granted/i.test(error.message || '')) {
      const rejected = state.selectedMailbox;
      state.selectedMailbox = '';
      await loadMailboxPage(1, nextQuery);
      state.mailboxError = `You do not have access to ${rejected}.`;
      renderMessageList();
      return;
    }
    renderErrorState('Mailbox unavailable', 'We could not load the mailbox right now.');
  }
}

function exactGrantAddresses() {
  return (state.me?.grants || []).filter((g) => g.kind === 'exact').map((g) => g.pattern);
}

// renderMailboxPicker lets a user narrow the inbox to one mailbox. Non-admins
// choose from their exact grants; admins (and users with domain grants) can
// type any address. Returns null when there is nothing to choose.
function renderMailboxPicker() {
  const me = state.me;
  if (!me) {
    return null;
  }
  const exact = exactGrantAddresses();
  const hasDomain = (me.grants || []).some((g) => g.kind === 'domain');
  if (!me.is_admin && !hasDomain && exact.length < 2) {
    return null;
  }

  const wrap = el('div', 'mailbox-picker');
  const label = el('label', 'message-search-label', 'Mailbox');
  label.setAttribute('for', 'mailbox-picker-input');
  const apply = (value) => {
    const next = (value || '').trim().toLowerCase();
    if (next === state.selectedMailbox) {
      return;
    }
    state.selectedMailbox = next;
    state.mailboxError = '';
    void loadMailboxPage(1, state.currentQuery || '');
  };

  if (!me.is_admin && !hasDomain) {
    const select = el('select', 'form-input mailbox-picker-select');
    select.id = 'mailbox-picker-input';
    select.appendChild(new Option('All my mailboxes', ''));
    exact.forEach((address) => select.appendChild(new Option(address, address)));
    select.value = state.selectedMailbox;
    select.addEventListener('change', () => apply(select.value));
    wrap.append(label, select);
    return wrap;
  }

  const row = el('div', 'mailbox-picker-row');
  const input = el('input', 'form-input mailbox-picker-input');
  Object.assign(input, {
    id: 'mailbox-picker-input',
    type: 'text',
    inputMode: 'email',
    autocomplete: 'off',
    spellcheck: false,
    placeholder: me.is_admin ? 'All mailboxes; type an address to filter' : 'All my mailboxes; type an address to filter',
    value: state.selectedMailbox,
  });
  input.setAttribute('list', 'mailbox-picker-options');
  const options = el('datalist');
  options.id = 'mailbox-picker-options';
  exact.forEach((address) => options.appendChild(new Option(address, address)));
  input.addEventListener('change', () => apply(input.value));
  input.addEventListener('keydown', (event) => {
    if (event.key === 'Enter') {
      apply(input.value);
    }
  });
  row.append(input, options);
  if (state.selectedMailbox) {
    const clear = createButton('Show all', 'tertiary-button', () => apply(''));
    clear.setAttribute('aria-label', 'Show all mailboxes');
    row.appendChild(clear);
  }
  wrap.append(label, row);
  if (state.mailboxError) {
    const error = el('p', 'inline-error', state.mailboxError);
    error.setAttribute('role', 'alert');
    input.setAttribute('aria-invalid', 'true');
    wrap.appendChild(error);
  }
  if (!me.is_admin) {
    const scope = el('p', 'section-meta mailbox-picker-scope');
    scope.textContent = 'You can read: ' + (me.grants || []).map((g) => g.pattern).join(', ');
    wrap.appendChild(scope);
  }
  return wrap;
}

function renderNoMailboxAssigned(notice = '') {
  const shell = renderShell('Access pending', 'You are signed in, but no mailbox has been shared with you yet.');
  const check = createButton('Check again', 'primary-button', async () => {
    check.disabled = true;
    try {
      state.me = (await window.API.me()) || state.me;
    } catch (error) {
      // Fall through and re-render with what we have.
    }
    if (state.me && (state.me.is_admin || (state.me.grants || []).length > 0)) {
      await loadMailboxPage(1, '', { showLoading: true });
      return;
    }
    renderNoMailboxAssigned('Still no mailbox. Try again after an administrator confirms.');
  });
  const card = renderStatusCard(
    'No mailbox assigned',
    `No mailbox has been assigned to ${state.me?.email || 'your account'}. Ask an administrator to grant access, then check again.`,
    [check],
  );
  if (notice) {
    const status = el('p', 'section-meta', notice);
    status.setAttribute('role', 'status');
    card.appendChild(status);
  }
  shell.appendChild(card);
}

function renderMessageList(messages = state.mailbox || []) {
  if (state.me && !state.me.is_admin && (state.me.grants || []).length === 0) {
    renderNoMailboxAssigned();
    return;
  }
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

  const titleGroup = document.createElement('div');
  titleGroup.className = 'section-title-group';
  titleGroup.append(title, summary);

  const mailboxPicker = renderMailboxPicker();

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
  listHeader.append(titleGroup);
  if (mailboxPicker) {
    listHeader.appendChild(mailboxPicker);
  }
  listPanel.append(listHeader, searchBlock);

  if (state.logoutError) {
    const logoutError = document.createElement('div');
    logoutError.className = 'form-error';
    logoutError.setAttribute('role', 'alert');
    logoutError.textContent = state.logoutError;
    listPanel.appendChild(logoutError);
  }

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
    } else if (state.selectedMailbox) {
      emptyTitle.textContent = 'No messages for this mailbox';
      emptyMessage.textContent = `Nothing has arrived for ${state.selectedMailbox} yet.`;
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

// ---------------------------------------------------------------------------
// Admin: users, admin flags and mailbox grants
// ---------------------------------------------------------------------------

function adminState() {
  if (!state.admin) {
    state.admin = { users: [], envAdmins: [], filter: '', error: '', notice: '', busy: false, suggestToken: 0, suggestTimer: null, focus: null };
  }
  return state.admin;
}

function sentenceCase(text) {
  const value = String(text || '').trim();
  return value ? value.charAt(0).toUpperCase() + value.slice(1) : value;
}

async function showAdminPanel() {
  if (!state.me?.is_admin) {
    const shell = renderShell('Not available', 'Only administrators can manage access.');
    shell.appendChild(renderStatusCard('Admins only', 'Your account cannot manage access.', [
      createButton('Back to mailbox', 'primary-button', goToInbox),
    ]));
    return;
  }
  renderShell('Manage access', 'Loading users…');
  await reloadAdminUsers();
}

async function reloadAdminUsers() {
  const admin = adminState();
  try {
    const payload = await window.API.adminListUsers();
    admin.users = payload?.users || [];
    admin.envAdmins = payload?.env_admins || [];
  } catch (error) {
    if (isUnauthorizedError(error)) {
      redirectToLogin();
      return;
    }
    admin.error = sentenceCase(error.message) || 'Could not load users.';
  }
  renderAdminPanel();
}

// runAdminAction executes a mutation, then refreshes the list and the
// signed-in user's own view (their grants or admin flag may have changed).
// When inline is given, a failure is shown next to the control that caused it
// and the panel is not re-rendered, so typed input and focus survive.
async function runAdminAction(action, notice, options = {}) {
  const admin = adminState();
  const { inline, focus } = options;
  admin.busy = true;
  admin.error = '';
  admin.notice = '';
  try {
    await action();
  } catch (error) {
    admin.busy = false;
    if (isUnauthorizedError(error)) {
      redirectToLogin();
      return;
    }
    const message = sentenceCase(error.message) || 'That did not work.';
    if (inline) {
      inline(message);
      return;
    }
    admin.error = message;
    renderAdminPanel();
    return;
  }
  admin.busy = false;
  admin.notice = notice || '';
  admin.focus = focus || null;
  try {
    state.me = (await window.API.me()) || state.me;
  } catch (error) {
    // Keep the previous identity; the list reload below reports real failures.
  }
  if (!state.me?.is_admin) {
    navigateTo(ROUTES.list);
    return;
  }
  await reloadAdminUsers();
}

function scheduleMailboxSuggestions(input, datalist) {
  const admin = adminState();
  window.clearTimeout(admin.suggestTimer);
  admin.suggestTimer = window.setTimeout(async () => {
    const raw = input.value.trim().toLowerCase();
    const domainOnly = raw.startsWith('*@');
    const query = domainOnly ? raw.slice(2) : raw;
    if (!domainOnly && query.length < 2) {
      return;
    }
    const token = ++admin.suggestToken;
    try {
      const { addresses = [], domains = [] } = (await window.API.adminMailboxes(query)) || {};
      if (token !== admin.suggestToken) {
        return;
      }
      const options = domains.map((d) => new Option(`All of ${d}`, `*@${d}`));
      if (!domainOnly) {
        options.push(...addresses.map((a) => new Option(a, a)));
      }
      datalist.replaceChildren(...options);
    } catch (error) {
      // Suggestions are optional; typing a full pattern still works.
    }
  }, 250);
}

function renderGrantChip(user, grant) {
  const chip = el('span', 'grant-chip');
  chip.dataset.kind = grant.kind;
  if (grant.kind === 'domain') {
    chip.append(el('span', 'grant-chip-kind', 'Domain'));
  }
  chip.append(el('span', 'grant-chip-label', grant.pattern));
  const remove = createButton('×', 'grant-chip-remove', () => {
    if (!window.confirm(`Remove ${user.email}'s access to ${grant.pattern}?`)) {
      return;
    }
    void runAdminAction(() => window.API.adminDeleteGrant(user.id, grant.id), `Removed ${grant.pattern} from ${user.email}.`,
      { focus: `grant-input-${user.id}` });
  });
  remove.setAttribute('aria-label', `Remove ${grant.pattern} from ${user.email}`);
  remove.disabled = adminState().busy;
  chip.appendChild(remove);
  return chip;
}

function renderGrantForm(user) {
  const form = el('form', 'grant-form');
  const input = el('input', 'form-input grant-input');
  const listId = `grant-options-${user.id}`;
  Object.assign(input, { id: `grant-input-${user.id}`, type: 'text', placeholder: 'Address or *@domain', required: true, autocomplete: 'off', spellcheck: false });
  input.setAttribute('list', listId);
  input.setAttribute('aria-label', `Grant a mailbox to ${user.email}`);
  const datalist = el('datalist');
  datalist.id = listId;
  const error = el('p', 'inline-error');
  error.id = `grant-error-${user.id}`;
  error.hidden = true;
  error.setAttribute('role', 'alert');
  input.setAttribute('aria-describedby', error.id);
  input.addEventListener('input', () => {
    error.hidden = true;
    input.removeAttribute('aria-invalid');
    scheduleMailboxSuggestions(input, datalist);
  });
  const submit = el('button', 'secondary-button', 'Grant');
  submit.type = 'submit';
  submit.disabled = adminState().busy;
  submit.setAttribute('aria-label', `Grant mailbox to ${user.email}`);
  form.append(input, datalist, submit, error);
  form.addEventListener('submit', (event) => {
    event.preventDefault();
    const pattern = input.value.trim();
    if (!pattern) {
      return;
    }
    submit.disabled = true;
    void runAdminAction(() => window.API.adminAddGrant(user.id, pattern), `Granted ${pattern} to ${user.email}.`, {
      focus: input.id,
      inline: (message) => {
        submit.disabled = false;
        error.textContent = message;
        error.hidden = false;
        input.setAttribute('aria-invalid', 'true');
        input.focus();
      },
    });
  });
  return form;
}

function renderAdminUserRow(user) {
  const admin = adminState();
  const isAdmin = user.is_admin || user.is_env_admin;
  const isSelf = user.email === state.me?.email;
  const row = el('article', 'admin-user');
  const head = el('div', 'admin-user-head');
  const identity = el('div', 'admin-user-identity');
  const email = el('strong', 'admin-user-email', isSelf ? `${user.email} (you)` : user.email);
  email.id = `admin-user-${user.id}`;
  row.setAttribute('aria-labelledby', email.id);
  identity.append(email);
  const lastLogin = user.last_login_at
    ? `Last sign-in ${formatDate(user.last_login_at)} via ${PROVIDER_LABELS[user.last_login_provider] || user.last_login_provider}`
    : 'Never signed in';
  identity.append(el('span', 'section-meta', lastLogin));

  const controls = el('div', 'admin-user-controls');
  const toggleLabel = el('label', 'admin-toggle');
  const toggle = el('input');
  toggle.type = 'checkbox';
  toggle.id = `admin-toggle-${user.id}`;
  toggle.checked = isAdmin;
  toggle.disabled = admin.busy || user.is_env_admin;
  toggle.setAttribute('aria-label', `${user.email} is an administrator`);
  toggle.addEventListener('change', () => {
    const next = toggle.checked;
    const question = next
      ? `Make ${user.email} an administrator? They will be able to read every mailbox and manage access.`
      : isSelf
        ? 'Remove your own administrator rights? You will lose access to this page.'
        : `Remove administrator rights from ${user.email}?`;
    if (!window.confirm(question)) {
      toggle.checked = !next;
      return;
    }
    void runAdminAction(() => window.API.adminSetAdmin(user.id, next), `${user.email} is ${next ? 'now' : 'no longer'} an administrator.`,
      { focus: toggle.id });
  });
  toggleLabel.append(toggle, el('span', '', user.is_env_admin ? 'Admin (set by ADMIN_EMAILS)' : 'Admin'));

  const remove = createButton('Remove', 'tertiary-button admin-delete', () => {
    if (!window.confirm(`Remove ${user.email} and all of their mailbox grants?\n\nThis does not block sign-in: if they sign in again they reappear here with no access.`)) {
      return;
    }
    void runAdminAction(() => window.API.adminDeleteUser(user.id), `Removed ${user.email}.`);
  });
  remove.setAttribute('aria-label', `Remove ${user.email}`);
  remove.disabled = admin.busy || user.is_env_admin || isSelf;
  if (user.is_env_admin || isSelf) {
    remove.title = isSelf ? 'You cannot remove yourself.' : 'Set by ADMIN_EMAILS; change the server configuration instead.';
  }
  controls.append(toggleLabel, remove);
  head.append(identity, controls);
  row.appendChild(head);

  const grants = el('div', 'grant-list');
  if (isAdmin) {
    grants.append(el('span', 'section-meta', 'Administrators can read every mailbox.'));
  }
  (user.grants || []).forEach((grant) => grants.appendChild(renderGrantChip(user, grant)));
  if (!isAdmin && (user.grants || []).length === 0) {
    grants.append(el('span', 'section-meta grant-empty', 'No mailbox access yet.'));
  }
  row.appendChild(grants);
  // Admins read everything; only offer the grant form when it changes something
  // (or when grants already exist and should stay editable after a demotion).
  if (!isAdmin || (user.grants || []).length > 0) {
    row.appendChild(renderGrantForm(user));
  }
  return row;
}

// renderAdminTabs switches between the users page and the audit log.
function renderAdminTabs(active) {
  const nav = el('nav', 'admin-tabs');
  nav.setAttribute('aria-label', 'Administration');
  [['users', 'Users and grants', ROUTES.admin], ['audit', 'Audit log', ROUTES.audit]].forEach(([key, label, href]) => {
    const link = createLinkButton(label, href, 'admin-tab');
    if (key === active) {
      link.setAttribute('aria-current', 'page');
    }
    nav.appendChild(link);
  });
  return nav;
}

function renderAdminPanel() {
  const admin = adminState();
  const shell = renderShell('Manage access', 'Choose which mailboxes each signed-in account can read.');
  shell.appendChild(renderAdminTabs('users'));

  const panel = el('section', 'panel admin-panel');
  const header = el('div', 'message-list-header');
  const titleGroup = el('div', 'section-title-group');
  const intro = el('p', 'section-meta admin-intro');
  intro.append('Grant an exact address such as ');
  intro.append(el('code', '', 'alice@example.com'));
  intro.append(' or a whole domain such as ');
  intro.append(el('code', '', '*@example.com'));
  intro.append('. People appear here after their first sign-in. Changes apply immediately.');
  titleGroup.append(el('h2', 'panel-title', 'Users'), intro);
  header.appendChild(titleGroup);
  panel.appendChild(header);

  if (admin.error) {
    const error = el('div', 'form-error', admin.error);
    error.setAttribute('role', 'alert');
    panel.appendChild(error);
  }
  if (admin.notice) {
    const notice = el('div', 'form-notice', admin.notice);
    notice.setAttribute('role', 'status');
    panel.appendChild(notice);
  }

  const addSection = el('div', 'admin-section');
  const addLabel = el('label', 'message-search-label', 'Add a user before they sign in');
  addLabel.setAttribute('for', 'admin-add-email');
  const addForm = el('form', 'admin-add-user');
  const addInput = el('input', 'form-input');
  Object.assign(addInput, { id: 'admin-add-email', type: 'email', placeholder: 'person@example.com', required: true, autocomplete: 'off' });
  const addError = el('p', 'inline-error');
  addError.hidden = true;
  addError.setAttribute('role', 'alert');
  const addButton = el('button', 'primary-button', 'Add user');
  addButton.type = 'submit';
  addButton.disabled = admin.busy;
  addForm.append(addInput, addButton, addError);
  addInput.addEventListener('input', () => { addError.hidden = true; });
  addForm.addEventListener('submit', (event) => {
    event.preventDefault();
    const email = addInput.value.trim();
    if (!email) {
      return;
    }
    addButton.disabled = true;
    void runAdminAction(() => window.API.adminCreateUser(email), `Added ${email}. Grant a mailbox below.`, {
      inline: (message) => {
        addButton.disabled = false;
        addError.textContent = message;
        addError.hidden = false;
        addInput.focus();
      },
    });
  });
  addSection.append(addLabel, addForm);

  const filterSection = el('div', 'admin-section');
  const filterLabel = el('label', 'message-search-label', 'Find a user or mailbox');
  filterLabel.setAttribute('for', 'admin-filter');
  const filter = el('input', 'message-search-input admin-filter');
  Object.assign(filter, { id: 'admin-filter', type: 'search', placeholder: 'Filter users…', value: admin.filter });
  filterSection.append(filterLabel, filter);
  panel.append(addSection, filterSection);

  const list = el('div', 'admin-user-list');
  const renderRows = () => {
    const needle = admin.filter.trim().toLowerCase();
    const rows = admin.users.filter((u) => !needle || u.email.includes(needle)
      || (u.grants || []).some((g) => g.pattern.includes(needle)));
    list.replaceChildren(...rows.map(renderAdminUserRow));
    if (rows.length === 0) {
      list.appendChild(el('p', 'muted-copy', admin.users.length ? 'No users match the filter.' : 'No users yet.'));
    }
  };
  filter.addEventListener('input', () => {
    admin.filter = filter.value;
    renderRows();
  });
  renderRows();
  panel.appendChild(list);

  if (admin.envAdmins.length > 0) {
    const env = el('p', 'section-meta admin-env-note');
    env.textContent = `Also administrators via ADMIN_EMAILS (not signed in yet): ${admin.envAdmins.join(', ')}`;
    panel.appendChild(env);
  }

  shell.appendChild(panel);

  const focusId = admin.focus;
  admin.focus = null;
  const target = focusId && document.getElementById(focusId);
  if (target) {
    target.focus();
  }
}

// ---------------------------------------------------------------------------
// Admin: audit log
// ---------------------------------------------------------------------------

const AUDIT_EVENT_LABELS = {
  sign_in: 'Signed in',
  sign_in_failed: 'Sign-in rejected',
  sign_out: 'Signed out',
  mailbox_list: 'Opened inbox',
  message_view: 'Read message',
  raw_download: 'Fetched raw source',
  attachment_download: 'Downloaded attachment',
  access_denied: 'Access denied',
  admin_user_create: 'Added user',
  admin_user_delete: 'Removed user',
  admin_role_change: 'Changed admin rights',
  admin_grant_add: 'Granted mailbox',
  admin_grant_remove: 'Removed mailbox grant',
};

const AUDIT_EVENT_GROUPS = [
  ['Sign-in', ['sign_in', 'sign_out']],
  ['Mail access', ['mailbox_list', 'message_view', 'raw_download', 'attachment_download']],
  ['Denied', ['access_denied', 'sign_in_failed']],
  ['Admin changes', ['admin_user_create', 'admin_user_delete', 'admin_role_change', 'admin_grant_add', 'admin_grant_remove']],
];

const AUDIT_DENIED = new Set(['access_denied', 'sign_in_failed']);

function auditState() {
  if (!state.audit) {
    state.audit = { filter: {}, dates: { from: '', to: '' }, events: [], nextBefore: 0, loading: false, error: '', token: 0, focusIndex: -1 };
  }
  return state.audit;
}

function auditFiltersActive(audit) {
  return Object.values(audit.filter).some((v) => v) || Boolean(audit.dates.from || audit.dates.to);
}

// localDayToUTC turns a local calendar date into the UTC instant it starts at.
function localDayToUTC(day, addDays = 0) {
  const [y, m, d] = day.split('-').map(Number);
  return new Date(y, m - 1, d + addDays).toISOString();
}

function auditQuery(audit) {
  const query = { ...audit.filter };
  if (audit.dates.from) {
    query.since = localDayToUTC(audit.dates.from);
  }
  if (audit.dates.to) {
    query.until = localDayToUTC(audit.dates.to, 1); // inclusive "to" date
  }
  return query;
}

async function showAuditLog() {
  if (!state.me?.is_admin) {
    await showAdminPanel();
    return;
  }
  renderShell('Audit log', 'Loading events…');
  await loadAudit(false);
}

// loadAudit fetches the first page for the current filter, or the next page
// when more is true. A newer request makes older responses irrelevant.
async function loadAudit(more) {
  const audit = auditState();
  if (more && audit.loading) {
    return;
  }
  const token = ++audit.token;
  audit.loading = true;
  audit.error = '';
  document.querySelectorAll('.audit-more, .audit-apply').forEach((button) => { button.disabled = true; });
  const previousCount = more ? audit.events.length : 0;
  try {
    const payload = await window.API.adminAudit(auditQuery(audit), more ? audit.nextBefore : 0);
    if (token !== audit.token) {
      return;
    }
    const events = payload?.events || [];
    audit.events = more ? audit.events.concat(events) : events;
    audit.nextBefore = payload?.next_before || 0;
    audit.focusIndex = more && events.length ? previousCount : -1;
  } catch (error) {
    if (token !== audit.token) {
      return;
    }
    if (isUnauthorizedError(error)) {
      redirectToLogin();
      return;
    }
    audit.error = sentenceCase(error.message) || 'Could not load the audit log.';
  }
  audit.loading = false;
  renderAuditLog();
}

function applyAuditFilter(patch) {
  const audit = auditState();
  audit.filter = { ...audit.filter, ...patch };
  void loadAudit(false);
}

function auditFilterChip(label, keys) {
  const chip = el('span', 'grant-chip audit-filter-chip');
  chip.append(el('span', 'grant-chip-label', label));
  const remove = createButton('×', 'grant-chip-remove', () => {
    const patch = {};
    keys.forEach((key) => { patch[key] = ''; });
    applyAuditFilter(patch);
  });
  remove.setAttribute('aria-label', `Clear filter: ${label}`);
  chip.appendChild(remove);
  return chip;
}

function renderAuditFilters() {
  const audit = auditState();
  const details = el('details', 'audit-filter-box');
  details.open = window.innerWidth > 768 || auditFiltersActive(audit);
  const activeCount = ['actor', 'mailbox', 'event'].filter((k) => audit.filter[k]).length
    + (audit.dates.from ? 1 : 0) + (audit.dates.to ? 1 : 0);
  details.appendChild(el('summary', 'audit-filter-summary', activeCount ? `Filters (${activeCount} active)` : 'Filters'));

  const form = el('form', 'audit-filters');
  const field = (name, labelText, type, value, placeholder, extra = {}) => {
    const wrap = el('label', 'audit-field');
    wrap.appendChild(el('span', 'message-search-label', labelText));
    const input = el('input', 'form-input');
    Object.assign(input, { name, type, placeholder: placeholder || '', value: value || '', autocomplete: 'off', spellcheck: false }, extra);
    if (extra.autocapitalize) {
      input.setAttribute('autocapitalize', extra.autocapitalize);
    }
    wrap.appendChild(input);
    return wrap;
  };
  const emailHints = { inputMode: 'email', autocapitalize: 'none' };
  const eventWrap = el('label', 'audit-field');
  eventWrap.appendChild(el('span', 'message-search-label', 'Event'));
  const select = el('select', 'form-input');
  select.name = 'event';
  select.appendChild(new Option('All events', ''));
  AUDIT_EVENT_GROUPS.forEach(([groupLabel, names]) => {
    const group = el('optgroup');
    group.label = groupLabel;
    names.forEach((name) => group.appendChild(new Option(AUDIT_EVENT_LABELS[name], name)));
    select.appendChild(group);
  });
  select.value = audit.filter.event || '';
  eventWrap.appendChild(select);

  form.append(
    field('actor', 'Who (login email)', 'text', audit.filter.actor, 'Email', emailHints),
    field('mailbox', 'Mailbox', 'text', audit.filter.mailbox, 'Address or domain', emailHints),
    eventWrap,
    field('from', 'From date', 'date', audit.dates.from),
    field('to', 'To date', 'date', audit.dates.to),
  );
  const actions = el('div', 'audit-filter-actions');
  const apply = el('button', 'primary-button audit-apply', 'Apply filters');
  apply.type = 'submit';
  apply.disabled = audit.loading;
  const reset = createButton('Reset', 'tertiary-button', () => {
    audit.filter = {};
    audit.dates = { from: '', to: '' };
    void loadAudit(false);
  });
  reset.disabled = !auditFiltersActive(audit);
  actions.append(apply, reset);
  form.appendChild(actions);
  form.addEventListener('submit', (event) => {
    event.preventDefault();
    const data = new FormData(form);
    audit.filter = {
      session: audit.filter.session || '',
      actor: String(data.get('actor') || '').trim(),
      mailbox: String(data.get('mailbox') || '').trim(),
      event: String(data.get('event') || ''),
    };
    audit.dates = { from: String(data.get('from') || ''), to: String(data.get('to') || '') };
    void loadAudit(false);
  });
  details.appendChild(form);
  return details;
}

function formatAuditTime(value) {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'medium' }).format(parsed);
}

function renderAuditRow(event, index) {
  const audit = auditState();
  const row = el('li', 'audit-row');
  row.dataset.event = event.event;
  row.tabIndex = -1;
  row.dataset.index = String(index);

  const when = el('time', 'audit-time', formatAuditTime(event.time));
  when.dateTime = event.time;

  const who = el('div', 'audit-who');
  const isPSK = event.provider === 'admin_psk';
  if (event.actor && !isPSK && audit.filter.actor !== event.actor) {
    const actor = createButton(event.actor, 'audit-link', () => applyAuditFilter({ actor: event.actor }));
    actor.setAttribute('aria-label', `${event.actor}: show only this person`);
    who.appendChild(actor);
  } else {
    who.appendChild(el('span', 'audit-actor', isPSK ? 'Emergency admin key' : (event.actor || 'unknown')));
  }
  if (event.provider && !isPSK) {
    const provider = el('span', 'audit-provider', `via ${PROVIDER_LABELS[event.provider] || event.provider}`);
    who.appendChild(provider);
  }

  const what = el('div', 'audit-what');
  const label = el('strong', 'audit-event', AUDIT_EVENT_LABELS[event.event] || event.event);
  if (AUDIT_DENIED.has(event.event)) {
    const flag = el('span', 'audit-flag', '!');
    flag.setAttribute('aria-hidden', 'true');
    label.prepend(flag);
  }
  what.appendChild(label);
  const target = el('div', 'audit-target');
  if (event.mailbox) {
    if (audit.filter.mailbox === event.mailbox) {
      target.appendChild(el('span', 'audit-mailbox', event.mailbox));
    } else {
      const mailbox = createButton(event.mailbox, 'audit-link audit-mailbox', () => applyAuditFilter({ mailbox: event.mailbox }));
      mailbox.setAttribute('aria-label', `${event.mailbox}: show only this mailbox`);
      target.appendChild(mailbox);
    }
  } else if (event.event === 'mailbox_list') {
    target.appendChild(el('span', 'audit-mailbox', 'All their mailboxes'));
  }
  if (event.message_id) {
    const link = createLinkButton(`Open message #${event.message_id} →`, `${ROUTES.messagePrefix}${event.message_id}`, 'audit-link audit-message-link');
    target.appendChild(link);
  }
  if (target.childNodes.length) {
    what.appendChild(target);
  }
  if (event.detail) {
    what.appendChild(el('span', 'section-meta audit-detail', event.detail === 'admin' ? 'Admin access (all mailboxes)' : event.detail));
  }

  const meta = el('div', 'audit-meta');
  if (event.client_ip) {
    const ip = el('span', 'audit-ip', event.client_ip);
    if (event.user_agent) {
      ip.title = event.user_agent;
    }
    meta.appendChild(ip);
  }
  if (event.session_key && audit.filter.session !== event.session_key) {
    const session = createButton('This sign-in', 'audit-link', () => applyAuditFilter({ session: event.session_key }));
    session.setAttribute('aria-label', `Show every event of this sign-in by ${event.actor || 'unknown'}`);
    meta.appendChild(session);
  }

  row.append(when, who, what, meta);
  return row;
}

function renderAuditLog() {
  const audit = auditState();
  const shell = renderShell('Audit log', 'Who signed in, with which email, and which mailboxes and messages they opened.');
  shell.appendChild(renderAdminTabs('audit'));

  const panel = el('section', 'panel audit-panel');
  panel.appendChild(renderAuditFilters());

  if (audit.filter.session) {
    const active = el('div', 'grant-list audit-active-filters');
    const owner = audit.events.find((e) => e.session_key === audit.filter.session)?.actor;
    active.appendChild(auditFilterChip(owner ? `One sign-in by ${owner}` : 'One sign-in', ['session']));
    panel.appendChild(active);
  }

  if (audit.error) {
    const error = el('div', 'form-error', audit.error);
    error.setAttribute('role', 'alert');
    panel.appendChild(error);
  }

  const summary = el('p', 'section-meta audit-summary');
  summary.setAttribute('role', 'status');
  if (audit.events.length) {
    summary.textContent = `${audit.nextBefore ? `Newest ${audit.events.length} events; older ones below` : `${audit.events.length} event${audit.events.length === 1 ? '' : 's'}`}, newest first. Times are in your local time zone.`;
  } else {
    summary.textContent = auditFiltersActive(audit) ? 'No events match these filters.' : 'No audit events recorded yet.';
  }
  panel.appendChild(summary);

  if (audit.events.length) {
    const list = el('ol', 'audit-list');
    list.setAttribute('role', 'list');
    audit.events.forEach((event, index) => list.appendChild(renderAuditRow(event, index)));
    panel.appendChild(list);
  }
  if (audit.nextBefore) {
    const more = createButton(audit.loading ? 'Loading…' : 'Load older events', 'secondary-button audit-more', () => void loadAudit(true));
    more.disabled = audit.loading;
    panel.appendChild(more);
  }
  shell.appendChild(panel);

  if (audit.focusIndex >= 0) {
    panel.querySelector(`.audit-row[data-index="${audit.focusIndex}"]`)?.focus();
    audit.focusIndex = -1;
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
  if (hash === ROUTES.admin) {
    return { type: 'admin' };
  }
  if (hash === ROUTES.audit) {
    return { type: 'audit' };
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
  if (!state.me) {
    await renderLogin();
    return;
  }
  const route = parseRoute(window.location.hash);
  if (route.type === 'admin') {
    await showAdminPanel();
    return;
  }
  if (route.type === 'audit') {
    await showAuditLog();
    return;
  }
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

async function boot() {
  ensureAppRoot();
  window.addEventListener('lite-mail:unauthorized', redirectToLogin);
  window.addEventListener('hashchange', handleNavigation);
  try {
    state.me = await window.API.me();
  } catch (error) {
    renderErrorState('Service unavailable', 'We could not reach the mail service. Please reload the page.');
    return;
  }
  if (state.me && window.location.pathname === '/login') {
    history.replaceState(null, '', '/' + window.location.hash);
  }
  await handleNavigation();
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', () => void boot(), { once: true });
} else {
  void boot();
}

window.liteMailApp = {
  handleNavigation,
  renderMessageView,
  showMessageView,
  renderTextBody,
  renderRawSource,
  renderAttachments,
};
