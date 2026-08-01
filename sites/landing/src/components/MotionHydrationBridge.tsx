"use client";

import { useEffect } from "react";

const hydrationAttributeName = "data-agent-comms-hydrated";
const hydrationEventName = "agent-comms:hydrated";

export function MotionHydrationBridge() {
  useEffect(() => {
    document.documentElement.setAttribute(hydrationAttributeName, "true");
    window.dispatchEvent(new Event(hydrationEventName));
  }, []);

  return null;
}
