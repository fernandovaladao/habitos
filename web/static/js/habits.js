import { csrfToken } from "/static/js/firebase-client.js";

const form = document.querySelector("[data-habit-form]");

function updateConditionalFields() {
  if (!form) return;
  const quantitative = form.elements.goalType.value === "quantitative";
  form.querySelector("[data-quantitative]").hidden = !quantitative;
  form.elements.target.required = quantitative;
  const custom = quantitative && form.elements.unit.value === "other";
  form.querySelector("[data-custom-unit]").hidden = !custom;
  form.elements.customUnit.required = custom;
}

if (form) {
  form.addEventListener("change", updateConditionalFields);
  updateConditionalFields();
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const message = form.querySelector("[data-form-message]");
    const button = form.querySelector("button[type=submit]");
    button.disabled = true;
    message.textContent = "Salvando…";
    const id = form.dataset.habitId;
    const payload = {
      title: form.elements.title.value,
      description: form.elements.description.value,
      goalType: form.elements.goalType.value,
      target: form.elements.goalType.value === "quantitative" ? form.elements.target.value : "",
      unit: form.elements.goalType.value === "quantitative" ? form.elements.unit.value : "",
      customUnit: form.elements.goalType.value === "quantitative" && form.elements.unit.value === "other" ? form.elements.customUnit.value : "",
      weekdays: [...form.querySelectorAll('input[name="weekdays"]:checked')].map((input) => Number(input.value)),
      time: form.elements.time.value,
      weeklyTargetExecutions: Number(form.elements.weeklyTargetExecutions.value),
      reminder: form.elements.reminder.value,
      age: Number(form.elements.age.value),
      weight: form.elements.weight.value,
      height: form.elements.height.value,
      gender: form.elements.gender.value
    };
    try {
      await request(id ? `/api/habits/${id}` : "/api/habits", id ? "PUT" : "POST", payload);
      window.location.assign("/meus-habitos");
    } catch (error) {
      message.textContent = error.message;
      button.disabled = false;
    }
  });

  const suggestButton = form.querySelector("[data-ai-suggest]");
  const preview = document.querySelector("[data-ai-preview]");
  let currentSuggestion = null;
  if (suggestButton && preview) {
    suggestButton.addEventListener("click", async () => {
      const message = form.querySelector("[data-form-message]");
      if (!form.elements.title.checkValidity() || !form.elements.description.checkValidity()) {
        form.reportValidity();
        message.textContent = "Preencha um título e uma descrição válidos antes de pedir uma sugestão.";
        return;
      }
      suggestButton.disabled = true;
      message.textContent = "Gerando sugestão…";
      preview.hidden = true;
      try {
        currentSuggestion = await request("/api/habit-suggestions", "POST", {
          title: form.elements.title.value,
          description: form.elements.description.value
        });
        renderSuggestion(preview, currentSuggestion);
        preview.hidden = false;
        message.textContent = "Sugestão pronta. Revise antes de usar.";
      } catch (error) {
        currentSuggestion = null;
        message.textContent = error.message;
      } finally {
        suggestButton.disabled = false;
      }
    });
    preview.querySelector("[data-ai-ignore]").addEventListener("click", () => {
      currentSuggestion = null;
      preview.hidden = true;
      form.querySelector("[data-form-message]").textContent = "Sugestão ignorada. Seus campos foram preservados.";
    });
    preview.querySelector("[data-ai-apply]").addEventListener("click", () => {
      if (!currentSuggestion) return;
      applySuggestion(form, currentSuggestion);
      preview.hidden = true;
      form.querySelector("[data-form-message]").textContent = "Sugestão aplicada aos campos. Revise e salve quando estiver pronto.";
      currentSuggestion = null;
    });
  }
}

function renderSuggestion(preview, suggestion) {
  const unitLabels = { pages: "páginas", minutes: "minutos", kilometers: "km", times: "vezes", liters: "litros", other: suggestion.customUnit };
  const weekdayLabels = { 1: "Seg", 2: "Ter", 3: "Qua", 4: "Qui", 5: "Sex", 6: "Sáb", 7: "Dom" };
  preview.querySelector("[data-ai-title]").textContent = suggestion.title;
  preview.querySelector("[data-ai-description]").textContent = suggestion.description;
  preview.querySelector("[data-ai-goal]").textContent = suggestion.goalType === "quantitative" ? `${suggestion.target} ${unitLabels[suggestion.unit] || ""}`.trim() : "Meta simples";
  preview.querySelector("[data-ai-frequency]").textContent = `${suggestion.weekdays.map((day) => weekdayLabels[day]).join(", ")} · meta semanal ${suggestion.weeklyTargetExecutions}`;
  preview.querySelector("[data-ai-time]").textContent = suggestion.time || "Manter o horário preenchido";
}

function applySuggestion(form, suggestion) {
  form.elements.title.value = suggestion.title;
  form.elements.description.value = suggestion.description;
  form.elements.goalType.value = suggestion.goalType;
  form.elements.target.value = suggestion.goalType === "quantitative" ? suggestion.target : "";
  form.elements.unit.value = suggestion.goalType === "quantitative" ? suggestion.unit : "pages";
  form.elements.customUnit.value = suggestion.goalType === "quantitative" && suggestion.unit === "other" ? suggestion.customUnit : "";
  for (const input of form.querySelectorAll('input[name="weekdays"]')) input.checked = suggestion.weekdays.includes(Number(input.value));
  form.elements.weeklyTargetExecutions.value = suggestion.weeklyTargetExecutions;
  if (suggestion.time) form.elements.time.value = suggestion.time;
  updateConditionalFields();
}

document.addEventListener("click", async (event) => {
  const simple = event.target.closest("[data-simple-result]");
  if (simple) {
    simple.disabled = true;
    try { await request(`/api/executions/${simple.dataset.executionId}/simple`, "POST", { completed: simple.dataset.simpleResult === "true" }); window.location.reload(); }
    catch (error) { window.alert(error.message); simple.disabled = false; }
    return;
  }
  const noteButton = event.target.closest("[data-note-action]");
  if (noteButton) {
    const card = noteButton.closest("[data-note-id]"); const action = noteButton.dataset.noteAction;
    if (action === "delete" && !window.confirm("Excluir esta nota/reflexão?")) return;
    try { await request(`/api/notes/${card.dataset.noteId}`, action === "delete" ? "DELETE" : "PUT", action === "edit" ? { content: card.querySelector("textarea").value } : {}); window.location.reload(); }
    catch (error) { window.alert(error.message); }
    return;
  }
  const button = event.target.closest("[data-habit-action]");
  if (!button) return;
  const action = button.dataset.habitAction;
  if (action === "delete" && !window.confirm("Excluir este hábito? O histórico será preservado, mas ele não poderá ser reativado no MVP.")) return;
  button.disabled = true;
  const message = document.querySelector("[data-list-message]");
  try {
    await request(`/api/habits/${button.dataset.id}${action === "delete" ? "" : `/${action}`}`, action === "delete" ? "DELETE" : "POST", {});
    window.location.assign("/meus-habitos");
  } catch (error) {
    if (message) message.textContent = error.message;
    button.disabled = false;
  }
});

const executionForm = document.querySelector("[data-execution-form]");
if (executionForm) executionForm.addEventListener("submit", async (event) => { event.preventDefault(); const message=executionForm.querySelector("[data-form-message]");try{await request(`/api/executions/${executionForm.dataset.executionId}/quantitative`,"POST",{achieved:executionForm.elements.achieved.value});window.location.reload()}catch(error){message.textContent=error.message} });

const noteForm = document.querySelector("[data-note-form]");
if (noteForm) noteForm.addEventListener("submit",async(event)=>{event.preventDefault();const message=noteForm.querySelector("[data-form-message]");try{await request(`/api/habits/${noteForm.dataset.habitId}/notes`,"POST",{content:noteForm.elements.content.value,executionId:noteForm.elements.attachExecution.checked?noteForm.elements.executionId.value:""});window.location.reload()}catch(error){message.textContent=error.message}});

async function request(url, method, body) {
  const csrf = await csrfToken();
  const response = await fetch(url, { method, credentials: "same-origin", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf }, body: JSON.stringify(body) });
  if (!response.ok) throw new Error((await response.text()).trim() || "Não foi possível concluir a operação.");
  return response.json();
}
