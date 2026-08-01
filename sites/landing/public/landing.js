const scrolledHeaderThresholdPixels = 24;
const copyFeedbackDurationMilliseconds = 4_000;
const defaultInstallButtonLabel = "Copy command";
const defaultDownloadButtonLabel = "Copy";
const pointerCenterOffset = 0.5;
const minimumScrollableDistancePixels = 1;
const revealIntersectionThreshold = 0.12;
const revealRootMargin = "0px";
const activeMotionDelayMilliseconds = 1_400;
const reducedMotionMediaQuery = "(prefers-reduced-motion: reduce)";
const hydrationAttributeName = "data-agent-comms-hydrated";
const hydrationEventName = "agent-comms:hydrated";
const nextRuntimeScriptSelector = 'script[src^="/_next/static/chunks/"]';
const revealedClassName = "is-revealed";
const activeClassName = "is-active";
const readoutUpdatingClassName = "is-updating";
const revealSelector = "[data-reveal], [data-motion-stage]";
const scrollProgressProperty = "--scroll-progress";
const scrollHeadPositionProperty = "--scroll-head-position";
const activeMotionTimers = new WeakMap();

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
    return;
  }

  const downloadCopyButton = target.closest("[data-copy-command]");
  if (downloadCopyButton instanceof HTMLButtonElement) {
    await copyDownloadCommand(downloadCopyButton);
  }
});

let scrollUpdateFrame = 0;
let pageMotionInitialized = false;
let pageMotionInitializationScheduled = false;
let pageLoaded = document.readyState === "complete";
let frameworkHydrated = !document.querySelector(nextRuntimeScriptSelector)
  || document.documentElement.hasAttribute(hydrationAttributeName);

function updateViewportState() {
  const scrollOffset = window.scrollY;
  document.querySelector("[data-site-header]")?.toggleAttribute("data-scrolled", scrollOffset > scrolledHeaderThresholdPixels);
  if (scrollOffset <= 0) {
    document.documentElement.style.setProperty(scrollProgressProperty, "0");
    document.documentElement.style.setProperty(scrollHeadPositionProperty, "0%");
    return;
  }
  const scrollableDistance = Math.max(document.documentElement.scrollHeight - window.innerHeight, minimumScrollableDistancePixels);
  const scrollProgress = Math.min(Math.max(scrollOffset / scrollableDistance, 0), 1);
  document.documentElement.style.setProperty(scrollProgressProperty, scrollProgress.toFixed(4));
  document.documentElement.style.setProperty(scrollHeadPositionProperty, `${(scrollProgress * 100).toFixed(2)}%`);
}

function scheduleViewportUpdate() {
  if (!pageMotionInitialized) return;
  if (scrollUpdateFrame !== 0) return;
  scrollUpdateFrame = window.requestAnimationFrame(() => {
    scrollUpdateFrame = 0;
    updateViewportState();
  });
}

window.addEventListener("scroll", scheduleViewportUpdate, { passive: true });
window.addEventListener("resize", scheduleViewportUpdate, { passive: true });
window.addEventListener("pageshow", scheduleViewportUpdate);
window.addEventListener("load", markPageLoaded, { once: true });
window.addEventListener(hydrationEventName, markFrameworkHydrated, { once: true });
attemptPageMotionInitialization();

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
  restartReadoutAnimation(instrument);
}

async function copyInstallCommand(copyButton) {
  const installCommand = document.querySelector("[data-install-command]")?.textContent?.trim();
  if (!installCommand) return;
  await copyCommandText(copyButton, installCommand, defaultInstallButtonLabel);
}

async function copyDownloadCommand(copyButton) {
  const commandSourceID = copyButton.dataset.commandSource;
  if (!commandSourceID) return;
  const downloadCommand = document.getElementById(commandSourceID)?.textContent?.trim();
  if (!downloadCommand) return;
  await copyCommandText(copyButton, downloadCommand, defaultDownloadButtonLabel);
}

async function copyCommandText(copyButton, command, defaultLabel) {
  try {
    await navigator.clipboard.writeText(command);
    setCopyButtonLabel(copyButton, "Command copied");
    copyButton.dataset.copyState = "success";
  } catch {
    setCopyButtonLabel(copyButton, "Copy failed");
    copyButton.dataset.copyState = "failure";
  }
  window.setTimeout(() => {
    setCopyButtonLabel(copyButton, defaultLabel);
    delete copyButton.dataset.copyState;
  }, copyFeedbackDurationMilliseconds);
}

function setCopyButtonLabel(copyButton, label) {
  const labelElement = copyButton.querySelector("[data-copy-label]");
  if (labelElement) {
    labelElement.textContent = label;
    return;
  }
  copyButton.textContent = label;
}

function initializeRevealMotion() {
  const revealElements = [...document.querySelectorAll(revealSelector)];
  const prefersReducedMotion = window.matchMedia(reducedMotionMediaQuery).matches;
  if (prefersReducedMotion || !("IntersectionObserver" in window)) {
    revealElements.forEach(revealElement);
    return;
  }

  const revealObserver = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (entry.isIntersecting) {
        revealElement(entry.target);
        scheduleElementActivation(entry.target);
      } else {
        deactivateElement(entry.target);
      }
    }
  }, {
    rootMargin: revealRootMargin,
    threshold: revealIntersectionThreshold
  });

  window.requestAnimationFrame(() => {
    revealElements.forEach((element) => revealObserver.observe(element));
  });
}

function schedulePageMotionInitialization() {
  if (pageMotionInitializationScheduled || pageMotionInitialized) return;
  pageMotionInitializationScheduled = true;
  window.requestAnimationFrame(() => {
    window.requestAnimationFrame(() => {
      initializeRevealMotion();
      pageMotionInitialized = true;
      pageMotionInitializationScheduled = false;
      updateViewportState();
    });
  });
}

function markPageLoaded() {
  pageLoaded = true;
  attemptPageMotionInitialization();
}

function markFrameworkHydrated() {
  frameworkHydrated = true;
  attemptPageMotionInitialization();
}

function attemptPageMotionInitialization() {
  if (pageLoaded && frameworkHydrated) schedulePageMotionInitialization();
}

function revealElement(element) {
  element.classList.add(revealedClassName);
}

function scheduleElementActivation(element) {
  if (element.classList.contains(activeClassName) || activeMotionTimers.has(element)) return;
  const timer = window.setTimeout(() => {
    activeMotionTimers.delete(element);
    element.classList.add(activeClassName);
  }, activeMotionDelayMilliseconds);
  activeMotionTimers.set(element, timer);
}

function deactivateElement(element) {
  const timer = activeMotionTimers.get(element);
  if (timer !== undefined) {
    window.clearTimeout(timer);
    activeMotionTimers.delete(element);
  }
  element.classList.remove(activeClassName);
}

function restartReadoutAnimation(instrument) {
  const readout = instrument.querySelector(".protocol-readout");
  if (!(readout instanceof HTMLElement)) return;
  readout.classList.remove(readoutUpdatingClassName);
  window.requestAnimationFrame(() => {
    readout.classList.add(readoutUpdatingClassName);
    readout.addEventListener("animationend", () => {
      readout.classList.remove(readoutUpdatingClassName);
    }, { once: true });
  });
}

function setText(container, selector, value) {
  const target = container.querySelector(selector);
  if (target && value) target.textContent = value;
}
