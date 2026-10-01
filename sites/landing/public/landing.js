const scrolledHeaderThresholdPixels = 24;
const copyFeedbackDurationMilliseconds = 4_000;
const defaultDownloadButtonLabel = "Copy";
// Tall sections must reveal when they enter even on a short mobile viewport,
// so the threshold stays tiny; the bottom margin makes content reveal once
// it is a little way into the viewport rather than at its very edge, where
// the motion would finish before anyone could see it.
const revealIntersectionThreshold = 0.01;
const revealRootMargin = "0px 0px -10% 0px";
const activeMotionDelayMilliseconds = 1_400;
const reducedMotionMediaQuery = "(prefers-reduced-motion: reduce)";
const hydrationAttributeName = "data-agent-comms-hydrated";
const hydrationEventName = "agent-comms:hydrated";
const nextRuntimeScriptSelector = 'script[src^="/_next/static/chunks/"]';
const revealedClassName = "is-revealed";
const activeClassName = "is-active";
const revealSelector = "[data-reveal]";
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

  const downloadCopyButton = target.closest("[data-copy-command]");
  if (downloadCopyButton instanceof HTMLButtonElement) {
    await copyDownloadCommand(downloadCopyButton);
    return;
  }

  const backButton = target.closest("[data-back-button]");
  if (backButton instanceof HTMLButtonElement) {
    if (window.history.length > 1) {
      window.history.back();
    } else {
      window.location.href = "/";
    }
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
