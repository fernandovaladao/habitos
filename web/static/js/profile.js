import { csrfToken, EmailAuthProvider, firebaseAuth, reauthenticateWithCredential, signOut, updateProfile } from "/static/js/firebase-client.js";

const form = document.querySelector("[data-profile-form]");
const message = form?.querySelector("[data-form-message]");

if (form) {
  const timezone = form.elements.timezone;
  if (!timezone.value || timezone.value === "UTC") {
    timezone.value = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  }
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const button = form.querySelector("button[type=submit]");
    button.disabled = true;
    message.textContent = "Salvando…";
    try {
      const csrf = await csrfToken();
      await updateProfile({
        nickname: form.elements.nickname.value,
        age: Number(form.elements.age.value),
        timezone: form.elements.timezone.value,
        rankingOptIn: form.elements.rankingOptIn.checked,
        weight: form.elements.weight.value,
        height: form.elements.height.value,
        gender: form.elements.gender.value,
        reminderNotificationEnabled: form.elements.reminderNotificationEnabled.checked,
        reminderEmailEnabled: form.elements.reminderEmailEnabled.checked
      }, csrf);
      message.textContent = "Perfil atualizado.";
    } catch (error) {
      message.textContent = error.message;
    } finally {
      button.disabled = false;
    }
  });
}

const avatarMessage = document.querySelector("[data-avatar-message]");
const photoForm = document.querySelector("[data-photo-form]");
const internalAvatarForm = document.querySelector("[data-internal-avatar-form]");

if (photoForm) photoForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = photoForm.querySelector("button[type=submit]");
  button.disabled = true;
  avatarMessage.textContent = "Enviando foto…";
  try {
    const csrf = await csrfToken();
    const body = new FormData(photoForm);
    const response = await fetch("/api/profile/photo", { method: "POST", credentials: "same-origin", headers: { "X-CSRF-Token": csrf }, body });
    if (!response.ok) throw new Error((await response.text()).trim() || "Não foi possível enviar a foto.");
    window.location.reload();
  } catch (error) {
    avatarMessage.textContent = error.message;
    button.disabled = false;
  }
});

if (internalAvatarForm) internalAvatarForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = internalAvatarForm.querySelector("button[type=submit]");
  button.disabled = true;
  avatarMessage.textContent = "Atualizando avatar…";
  try {
    const csrf = await csrfToken();
    const response = await fetch("/api/profile/avatar/internal", { method: "PUT", credentials: "same-origin", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf }, body: JSON.stringify({ avatarType: internalAvatarForm.elements.avatarType.value }) });
    if (!response.ok) throw new Error((await response.text()).trim() || "Não foi possível atualizar o avatar.");
    window.location.reload();
  } catch (error) {
    avatarMessage.textContent = error.message;
    button.disabled = false;
  }
});

document.querySelector("[data-remove-photo]")?.addEventListener("click", async (event) => {
  event.currentTarget.disabled = true;
  avatarMessage.textContent = "Removendo foto…";
  try {
    const csrf = await csrfToken();
    const response = await fetch("/api/profile/photo", { method: "DELETE", credentials: "same-origin", headers: { "X-CSRF-Token": csrf } });
    if (!response.ok) throw new Error((await response.text()).trim() || "Não foi possível remover a foto.");
    window.location.reload();
  } catch (error) {
    avatarMessage.textContent = error.message;
    event.currentTarget.disabled = false;
  }
});

const deletionDialog = document.querySelector("[data-account-deletion-dialog]");
const deletionForm = document.querySelector("[data-account-deletion-form]");
document.querySelector("[data-open-account-deletion]")?.addEventListener("click", () => deletionDialog.showModal());
document.querySelector("[data-cancel-account-deletion]")?.addEventListener("click", () => deletionDialog.close());

if (deletionForm) deletionForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = deletionForm.querySelector("button[type=submit]");
  const message = deletionForm.querySelector("[data-deletion-message]");
  button.disabled = true;
  message.textContent = "Confirmando sua identidade…";
  try {
    const firebase = await firebaseAuth();
    const user = firebase.currentUser;
    if (!user?.email) throw new Error("Entre novamente para excluir sua conta.");
    await reauthenticateWithCredential(user, EmailAuthProvider.credential(user.email, deletionForm.elements.password.value));
    const idToken = await user.getIdToken(true);
    const csrf = await csrfToken();
    const response = await fetch("/api/account/deletion/start", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf }, body: JSON.stringify({ confirmation: deletionForm.elements.confirmation.value, idToken }) });
    if (!response.ok && response.status !== 202) throw new Error((await response.text()).trim() || "Não foi possível iniciar a exclusão.");
    const result = await response.json();
    if (result.complete) {
      try { await signOut(firebase); } catch {}
      window.location.assign("/?conta-excluida=1");
      return;
    }
    window.location.assign("/exclusao-conta");
  } catch (error) {
    message.textContent = error.message || "Não foi possível iniciar a exclusão.";
    button.disabled = false;
  }
});
