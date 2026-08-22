import { csrfToken, firebaseAuth, friendlyAuthError, logoutSession, signInWithEmailAndPassword, signOut, updatePassword } from "/static/js/firebase-client.js";

const form = document.querySelector("[data-password-form]");
const message = form?.querySelector("[data-form-message]");

if (form) {
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const button = form.querySelector("button[type=submit]");
    button.disabled = true;
    message.textContent = "Alterando…";
    try {
      const auth = await firebaseAuth();
      const credential = await signInWithEmailAndPassword(auth, form.dataset.email, form.elements.currentPassword.value);
      await updatePassword(credential.user, form.elements.newPassword.value);
      await signOut(auth);
      const csrf = await csrfToken();
      await logoutSession(csrf);
      window.location.assign("/entrar");
    } catch (error) {
      message.textContent = friendlyAuthError(error);
    } finally {
      button.disabled = false;
    }
  });
}

