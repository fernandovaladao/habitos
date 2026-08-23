const CACHE_NAME = "habitos-static-v4";
const STATIC_ASSETS = [
  "/static/css/app.css",
  "/static/js/app.js",
  "/static/js/firebase-client.js",
  "/static/js/auth.js",
  "/static/js/profile.js",
	"/static/js/account-deletion.js",
  "/static/js/habits.js",
  "/static/js/password.js",
  "/static/js/session-actions.js",
  "/static/icons/icon.svg",
  "/static/manifest.webmanifest"
];

self.addEventListener("install", (event) => {
  event.waitUntil(caches.open(CACHE_NAME).then((cache) => cache.addAll(STATIC_ASSETS)));
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((key) => key !== CACHE_NAME).map((key) => caches.delete(key))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (event) => {
  if (event.request.method !== "GET" || !new URL(event.request.url).pathname.startsWith("/static/")) {
    return;
  }

  event.respondWith(
    caches.match(event.request).then((cached) => cached || fetch(event.request))
  );
});

self.addEventListener("push", (event) => {
  let message = { title: "Hora do seu hábito", body: "Abra o HÁBITOS para conferir o que está programado.", url: "/meus-habitos?filtro=today" };
  try { if (event.data) message = { ...message, ...event.data.json() }; } catch {}
  event.waitUntil(self.registration.showNotification(message.title, { body: message.body, icon: "/static/icons/icon.svg", badge: "/static/icons/icon.svg", data: { url: "/meus-habitos?filtro=today" } }));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  event.waitUntil(self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((clients) => {
    for (const client of clients) { if ("focus" in client) { client.navigate(event.notification.data.url); return client.focus(); } }
    return self.clients.openWindow(event.notification.data.url);
  }));
});
