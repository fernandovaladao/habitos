(() => {
  "use strict";

  if ("serviceWorker" in navigator) {
    window.addEventListener("load", () => {
      navigator.serviceWorker.register("/service-worker.js").catch(() => {
        // A interface continua funcional quando o navegador não permite o registro.
      });
    });
  }
})();

