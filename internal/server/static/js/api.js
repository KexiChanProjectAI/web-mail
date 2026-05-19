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
    return parseResponse(response);
  }

  window.API = {
    login: function (psk, email) {
      return request("/api/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ psk: psk, email: email || "" }),
      });
    },

    logout: function () {
      return request("/api/logout", { method: "POST" });
    },

    listMessages: function (page, perPage, query) {
      const params = new URLSearchParams({
        page: String(page || 1),
        per_page: String(perPage || 20),
      });
      if (query && query.trim()) {
        params.set("q", query.trim());
      }
      return request("/api/messages?" + params.toString(), { method: "GET" });
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
