import { csrfToken, firebaseAuth, signOut } from "/static/js/firebase-client.js";

const button = document.querySelector("[data-continue-deletion]");
const message = document.querySelector("[data-deletion-progress-message]");

async function continueDeletion() {
  button.disabled = true;
  message.textContent = "Continuando a exclusão…";
  try {
    const csrf = await csrfToken();
    const response = await fetch("/api/account/deletion/continue", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf }, body: "{}" });
    if (!response.ok && response.status !== 202) throw new Error((await response.text()).trim() || "Não foi possível continuar agora.");
    const result = await response.json();
    if (result.complete) {
      try { await signOut(await firebaseAuth()); } catch {}
      window.location.assign("/?conta-excluida=1");
      return;
    }
    window.setTimeout(continueDeletion, 100);
  } catch (error) {
    message.textContent = error.message || "Não foi possível continuar agora. Tente novamente.";
    button.disabled = false;
  }
}

button?.addEventListener("click", continueDeletion);
if (button) continueDeletion();
