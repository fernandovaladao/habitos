import { csrfToken, updateProfile } from "/static/js/firebase-client.js";

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
