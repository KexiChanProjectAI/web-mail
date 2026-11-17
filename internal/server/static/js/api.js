(function () {
  function buildErrorMessage(payload, fallback) {
    if (!payload) return fallback;
    if (typeof payload.error === "string" && payload.error.trim()) return payload.error;
    if (typeof payload.message === "string" && payload.message.trim()) return payload.message;
    return fallback;
  }

  async function parseResponse(response) {
    const contentType = response.headers.get("content-type") || "";
    const isJSON = contentType.includes("application/json");
    const payload = isJSON ? await response.json().catch(function () { return null; }) : null;

    if (response.status === 401) {
      window.dispatchEvent(new CustomEvent("lite-mail:unauthorized"));
      throw new Error(buildErrorMessage(payload, "Unauthorized"));
    }

    if (!response.ok) {
      throw new Error(buildErrorMessage(payload, "Request failed"));
    }

    return payload;
  }

  async function request(path, options) {
    const response = await fetch(path, Object.assign({ credentials: "same-origin" }, options || {}));
    if (response.status === 204) {
      return null;
    }
    return parseResponse(response);
  }

  function sendJSON(method, path, body) {
    return request(path, {
      method: method,
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body || {}),
    });
  }

  window.API = {
    providers: async function () {
      const response = await fetch("/api/auth/providers", { credentials: "same-origin" });
      if (!response.ok) {
        throw new Error("Could not load sign-in options");
      }
      return response.json();
    },

    // me resolves to null when nobody is signed in, without firing the
    // unauthorized event (the caller decides to show the login page).
    me: async function () {
      const response = await fetch("/api/me", { credentials: "same-origin", cache: "no-store" });
      if (response.status === 401) {
        return null;
      }
      if (!response.ok) {
        throw new Error("Could not load your account");
      }
      return response.json();
    },

    // login handles its own 401 (wrong key) instead of firing the global
    // unauthorized event, which would re-render the login page.
    login: async function (psk) {
      const response = await fetch("/api/login", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ psk: psk }),
      });
      const payload = await response.json().catch(function () { return null; });
      if (!response.ok) {
        throw new Error(buildErrorMessage(payload, "Sign-in failed"));
      }
      return payload;
    },

    logout: function () {
      return sendJSON("POST", "/api/logout", {});
    },

    listMessages: function (page, perPage, query, mailbox) {
      const params = new URLSearchParams({
        page: String(page || 1),
        per_page: String(perPage || 20),
      });
      if (query && query.trim()) {
        params.set("q", query.trim());
      }
      if (mailbox && mailbox.trim()) {
        params.set("mailbox", mailbox.trim());
      }
      return request("/api/messages?" + params.toString(), { method: "GET" });
    },

    adminListUsers: function () {
      return request("/api/admin/users", { method: "GET" });
    },

    adminCreateUser: function (email) {
      return sendJSON("POST", "/api/admin/users", { email: email });
    },

    adminSetAdmin: function (userId, isAdmin) {
      return sendJSON("PATCH", "/api/admin/users/" + encodeURIComponent(userId), { is_admin: !!isAdmin });
    },

    adminDeleteUser: function (userId) {
      return request("/api/admin/users/" + encodeURIComponent(userId), { method: "DELETE" });
    },

    adminAddGrant: function (userId, pattern) {
      return sendJSON("POST", "/api/admin/users/" + encodeURIComponent(userId) + "/grants", { pattern: pattern });
    },

    adminDeleteGrant: function (userId, grantId) {
      return request("/api/admin/users/" + encodeURIComponent(userId) + "/grants/" + encodeURIComponent(grantId), { method: "DELETE" });
    },

    adminMailboxes: function (query) {
      const params = new URLSearchParams();
      if (query && query.trim()) {
        params.set("q", query.trim());
      }
      return request("/api/admin/mailboxes?" + params.toString(), { method: "GET" });
    },

    getMessage: function (id) {
      return request("/api/messages/" + encodeURIComponent(id), { method: "GET" });
    },

    getAttachment: async function (messageId, index) {
      const response = await fetch("/api/messages/" + encodeURIComponent(messageId) + "/attachments/" + encodeURIComponent(index), {
        method: "GET",
        credentials: "same-origin",
      });

      if (response.status === 401) {
        window.dispatchEvent(new CustomEvent("lite-mail:unauthorized"));
        throw new Error("Unauthorized");
      }

      if (!response.ok) {
        throw new Error("Failed to download attachment");
      }

      const blob = await response.blob();
      return URL.createObjectURL(blob);
    },

    getRawMIME: async function (id) {
      const response = await fetch("/api/messages/" + encodeURIComponent(id) + "/raw", {
        method: "GET",
        credentials: "same-origin",
      });

      if (response.status === 401) {
        window.dispatchEvent(new CustomEvent("lite-mail:unauthorized"));
        throw new Error("Unauthorized");
      }

      if (!response.ok) {
        throw new Error("Failed to download raw message");
      }

      return response.blob();
    },
  };
})();
