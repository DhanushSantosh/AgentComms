const scrolledHeaderThresholdPixels = 24;
const copyFeedbackDurationMilliseconds = 4_000;
const defaultInstallButtonLabel = "Copy command";
const pointerCenterOffset = 0.5;

document.addEventListener("click", async (event) => {
  const target = event.target;
  if (!(target instanceof Element)) return;

  const menuToggle = target.closest("[data-menu-toggle]");
  if (menuToggle) {
    const navigation = document.querySelector("[data-site-navigation]");
    const opening = menuToggle.getAttribute("aria-expanded") !== "true";
    menuToggle.setAttribute("aria-expanded", String(opening));
    navigation?.toggleAttribute("data-open", opening);
    return;
  }

  if (target.closest("[data-site-navigation] a")) {
    document.querySelector("[data-menu-toggle]")?.setAttribute("aria-expanded", "false");
    document.querySelector("[data-site-navigation]")?.removeAttribute("data-open");
    return;
  }

  const collisionButton = target.closest("[data-collision-mode]");
  if (collisionButton instanceof HTMLElement) {
    updateCollisionLab(collisionButton);
    return;
  }

  const stageButton = target.closest("[data-stage]");
  if (stageButton instanceof HTMLElement) {
    updateProtocolInstrument(stageButton);
    return;
  }

  const copyButton = target.closest("[data-copy-install]");
  if (copyButton instanceof HTMLButtonElement) {
    await copyInstallCommand(copyButton);
  }
});

function updateScrolledHeader() {
  document.querySelector("[data-site-header]")?.toggleAttribute("data-scrolled", window.scrollY > scrolledHeaderThresholdPixels);
}

window.addEventListener("scroll", updateScrolledHeader, { passive: true });
updateScrolledHeader();

document.addEventListener("pointermove", (event) => {
  const target = event.target;
  if (!(target instanceof Element)) return;
  const field = target.closest("[data-coordination-field]");
  if (!(field instanceof HTMLElement)) return;
  const bounds = field.getBoundingClientRect();
  const horizontalPosition = (event.clientX - bounds.left) / bounds.width - pointerCenterOffset;
  const verticalPosition = (event.clientY - bounds.top) / bounds.height - pointerCenterOffset;
  field.style.setProperty("--pointer-x", horizontalPosition.toFixed(3));
  field.style.setProperty("--pointer-y", verticalPosition.toFixed(3));
}, { passive: true });

document.addEventListener("pointerout", (event) => {
  const target = event.target;
  if (!(target instanceof Element)) return;
  const field = target.closest("[data-coordination-field]");
  if (!(field instanceof HTMLElement)) return;
  const relatedTarget = event.relatedTarget;
  if (relatedTarget instanceof Node && field.contains(relatedTarget)) return;
  field.style.setProperty("--pointer-x", "0");
  field.style.setProperty("--pointer-y", "0");
}, { passive: true });

function updateCollisionLab(button) {
  const mode = button.dataset.collisionMode;
  if (mode !== "governed" && mode !== "ungoverned") return;
  const collisionLab = button.closest("[data-collision-lab]");
  if (!(collisionLab instanceof HTMLElement)) return;
  collisionLab.dataset.mode = mode;
  collisionLab.querySelectorAll("[data-collision-mode]").forEach((candidate) => {
    candidate.setAttribute("aria-pressed", String(candidate === button));
  });
  setText(collisionLab, "[data-collision-state]", mode === "governed" ? "RESOLVED" : "COLLISION");
  setText(collisionLab, "[data-proof-outcome]", mode === "governed" ? "one owner" : "two writers");
}

function updateProtocolInstrument(button) {
  const instrument = button.closest("[data-protocol-instrument]");
  if (!(instrument instanceof HTMLElement)) return;
  const buttons = [...instrument.querySelectorAll("[data-stage]")];
  buttons.forEach((candidate) => {
    const active = candidate === button;
    candidate.classList.toggle("is-active", active);
    candidate.setAttribute("aria-pressed", String(active));
  });
  const stageIndex = buttons.indexOf(button);
  const stageName = button.querySelector("span")?.textContent;
  setText(instrument, "[data-stage-sequence]", `${String(stageIndex + 1).padStart(2, "0")} / ${String(buttons.length).padStart(2, "0")}`);
  setText(instrument, "[data-stage-name]", stageName);
  setText(instrument, "[data-stage-description]", button.dataset.description);
  setText(instrument, "[data-stage-proves]", button.dataset.proves);
  setText(instrument, "[data-stage-excludes]", button.dataset.excludes);
  setText(instrument, "[data-stage-event]", button.dataset.event);
}

async function copyInstallCommand(copyButton) {
  const installCommand = document.querySelector("[data-install-command]")?.textContent?.trim();
  if (!installCommand) return;
  try {
    await navigator.clipboard.writeText(installCommand);
    copyButton.textContent = "Command copied";
  } catch {
    copyButton.textContent = "Copy failed";
  }
  window.setTimeout(() => {
    copyButton.textContent = defaultInstallButtonLabel;
  }, copyFeedbackDurationMilliseconds);
}

function setText(container, selector, value) {
  const target = container.querySelector(selector);
  if (target && value) target.textContent = value;
}
