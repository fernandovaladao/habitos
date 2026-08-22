import { csrfToken, logoutSession } from "/static/js/firebase-client.js";

document.querySelectorAll("[data-logout]").forEach((button) => {
  button.addEventListener("click", async () => {
    button.disabled = true;
    try {
      const csrf = await csrfToken();
      await logoutSession(csrf);
      window.location.assign("/entrar");
    } catch {
      button.disabled = false;
    }
  });
});

