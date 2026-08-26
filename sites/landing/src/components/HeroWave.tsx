"use client";

import { useEffect, useRef } from "react";

// rgb() of --cyan (#56d6c9) -- single-hue on purpose.
const CYAN_RGB = "86, 214, 201";
const DPR_CAP = 2;

export function HeroWave() {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvasEl = canvasRef.current;
    const parentEl = canvasEl?.parentElement;
    if (!canvasEl || !parentEl) return;
    const context2d = canvasEl.getContext("2d");
    if (!context2d) return;
    // Rebind to non-nullable consts -- TS doesn't carry the narrowing
    // above into the nested closures below.
    const canvas = canvasEl;
    const parent = parentEl;
    const ctx = context2d;

    const prefersReducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

    let width = 0;
    let height = 0;
    let frameId = 0;
    let t = 0;

    function resize() {
      const rect = parent.getBoundingClientRect();
      const dpr = Math.min(window.devicePixelRatio || 1, DPR_CAP);
      width = rect.width;
      height = rect.height;
      canvas.width = Math.round(width * dpr);
      canvas.height = Math.round(height * dpr);
      canvas.style.width = `${width}px`;
      canvas.style.height = `${height}px`;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    }

    function waveY(x: number, time: number) {
      const baseY = height * 0.74;
      return baseY
        + Math.sin(x * 0.006 + time) * height * 0.022
        + Math.sin(x * 0.014 - time * 1.4) * height * 0.012;
    }

    function draw() {
      ctx.clearRect(0, 0, width, height);

      ctx.beginPath();
      ctx.moveTo(0, height);
      ctx.lineTo(0, waveY(0, t));
      for (let x = 0; x <= width; x += 6) ctx.lineTo(x, waveY(x, t));
      ctx.lineTo(width, height);
      ctx.closePath();
      const fill = ctx.createLinearGradient(0, height * 0.69, 0, height);
      fill.addColorStop(0, `rgba(${CYAN_RGB}, 0.14)`);
      fill.addColorStop(1, `rgba(${CYAN_RGB}, 0.015)`);
      ctx.fillStyle = fill;
      ctx.fill();

      ctx.beginPath();
      ctx.moveTo(0, waveY(0, t));
      for (let x = 0; x <= width; x += 6) ctx.lineTo(x, waveY(x, t));
      ctx.strokeStyle = `rgba(${CYAN_RGB}, 0.4)`;
      ctx.lineWidth = 1.3;
      ctx.stroke();
    }

    function frame() {
      t += 0.006;
      draw();
      frameId = requestAnimationFrame(frame);
    }

    resize();
    window.addEventListener("resize", resize);

    if (prefersReducedMotion) {
      draw();
      return () => window.removeEventListener("resize", resize);
    }

    const handleVisibility = () => {
      if (document.hidden) {
        cancelAnimationFrame(frameId);
      } else {
        frameId = requestAnimationFrame(frame);
      }
    };
    document.addEventListener("visibilitychange", handleVisibility);
    frameId = requestAnimationFrame(frame);

    return () => {
      cancelAnimationFrame(frameId);
      window.removeEventListener("resize", resize);
      document.removeEventListener("visibilitychange", handleVisibility);
    };
  }, []);

  return <canvas ref={canvasRef} className="hero-wave" aria-hidden="true" />;
}
