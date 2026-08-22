import {
  createSession,
  createUserWithEmailAndPassword,
  csrfToken,
  ensureProfile,
  firebaseAuth,
  friendlyAuthError,
  sendPasswordResetEmail,
  signInWithEmailAndPassword,
  signOut,
  updateProfile
} from "/static/js/firebase-client.js";

const form = document.querySelector("[data-auth-form]");
const message = form?.querySelector("[data-form-message]");

if (form) {
  const timezoneField = form.elements.timezone;
  if (timezoneField) timezoneField.value = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  form.addEventListener("submit", handleSubmit);
}

async function handleSubmit(event) {
  event.preventDefault();
  setBusy(true);
  const mode = form.dataset.authForm;
  const data = new FormData(form);
  try {
    const auth = await firebaseAuth();
    if (mode === "password-reset") {
      await sendPasswordResetEmail(auth, data.get("email"));
      message.textContent = "Se o e-mail estiver cadastrado, você receberá as instruções de recuperação.";
      form.reset();
      return;
    }

    if (mode === "signup") {
      const nickname = data.get("nickname").trim();
      const nicknameLength = [...nickname].length;
      if (nicknameLength < 3 || nicknameLength > 24 || !/^[\p{L}\p{N} _-]+$/u.test(nickname)) {
        throw new Error("Apelido deve ter de 3 a 24 caracteres e usar apenas letras, números, espaços, _ ou -.");
      }
    }

    const credential = mode === "signup"
      ? await createUserWithEmailAndPassword(auth, data.get("email"), data.get("password"))
      : await signInWithEmailAndPassword(auth, data.get("email"), data.get("password"));
    const idToken = await credential.user.getIdToken();
    const csrf = await csrfToken();
    await createSession(idToken, csrf);
    await signOut(auth);

    const timezone = mode === "signup" ? data.get("timezone") : (Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC");
    const currentProfile = await ensureProfile(timezone, csrf);
    if (mode === "signup") {
      await updateProfile({
        nickname: data.get("nickname"),
        age: Number(data.get("age")),
        timezone,
        rankingOptIn: false
      }, csrf);
      window.location.assign("/perfil");
      return;
    }
    window.location.assign(currentProfile.profileComplete ? "/" : "/perfil");
  } catch (error) {
    message.textContent = friendlyAuthError(error);
  } finally {
    setBusy(false);
  }
}

function setBusy(busy) {
  const button = form.querySelector("button[type=submit]");
  button.disabled = busy;
  if (busy) message.textContent = "Aguarde…";
}
