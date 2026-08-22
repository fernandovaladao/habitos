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
        gender: form.elements.gender.value
      }, csrf);
      message.textContent = "Perfil atualizado.";
    } catch (error) {
      message.textContent = error.message;
    } finally {
      button.disabled = false;
    }
  });
}
