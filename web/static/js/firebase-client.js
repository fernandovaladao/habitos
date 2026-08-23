import { initializeApp } from "https://www.gstatic.com/firebasejs/12.17.1/firebase-app.js";
import {
  connectAuthEmulator,
  createUserWithEmailAndPassword,
  EmailAuthProvider,
  getAuth,
  inMemoryPersistence,
  sendPasswordResetEmail,
  reauthenticateWithCredential,
  setPersistence,
  signInWithEmailAndPassword,
  signOut,
  updatePassword
} from "https://www.gstatic.com/firebasejs/12.17.1/firebase-auth.js";

let authPromise;

export async function firebaseAuth() {
  if (!authPromise) {
    authPromise = fetch("/api/firebase-config", { credentials: "same-origin" })
      .then(requireOK)
      .then((response) => response.json())
      .then(async (config) => {
        const { authEmulatorUrl, ...firebaseConfig } = config;
        const app = initializeApp(firebaseConfig);
        const auth = getAuth(app);
        if (authEmulatorUrl) {
          connectAuthEmulator(auth, authEmulatorUrl, { disableWarnings: false });
        }
        await setPersistence(auth, inMemoryPersistence);
        return auth;
      });
  }
  return authPromise;
}

export { createUserWithEmailAndPassword, EmailAuthProvider, reauthenticateWithCredential, sendPasswordResetEmail, signInWithEmailAndPassword, signOut, updatePassword };

export async function csrfToken() {
  const response = await fetch("/api/auth/csrf", { credentials: "same-origin" });
  await requireOK(response);
  return (await response.json()).csrfToken;
}

export async function createSession(idToken, csrf) {
  return apiRequest("/api/auth/session", "POST", { idToken }, csrf);
}

export async function ensureProfile(timezone, csrf) {
  return apiRequest("/api/profile/ensure", "POST", { timezone }, csrf);
}

export async function updateProfile(profile, csrf) {
  return apiRequest("/api/profile", "PUT", profile, csrf);
}

export async function logoutSession(csrf) {
  return apiRequest("/api/auth/logout", "POST", {}, csrf);
}

async function apiRequest(url, method, body, csrf) {
  const response = await fetch(url, {
    method,
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
    body: JSON.stringify(body)
  });
  await requireOK(response);
  return response.json();
}

async function requireOK(response) {
  if (!response.ok) {
    const message = (await response.text()).trim();
    throw new Error(message || "Não foi possível concluir a operação.");
  }
  return response;
}

export function friendlyAuthError(error) {
  const messages = {
    "auth/email-already-in-use": "Este e-mail já está cadastrado.",
    "auth/invalid-credential": "E-mail ou senha inválidos.",
    "auth/invalid-email": "Informe um e-mail válido.",
    "auth/too-many-requests": "Muitas tentativas. Aguarde um pouco e tente novamente.",
    "auth/weak-password": "A senha precisa ter pelo menos 6 caracteres.",
    "auth/requires-recent-login": "Entre novamente para alterar sua senha."
  };
  return messages[error?.code] || error?.message || "Não foi possível concluir a operação.";
}
