"use client";

import { motion } from "framer-motion";
import "./LifecycleOrbit.css";

export function LifecycleOrbit() {
  return (
    <div className="lifecycle-orbit">
      {/* Background Starfield / Particles */}
      <div className="orbit-particles" aria-hidden="true">
        <svg viewBox="0 0 100 100">
          <circle cx="20" cy="30" r="0.2" fill="var(--steel-light)" opacity="0.5" />
          <circle cx="80" cy="20" r="0.3" fill="var(--steel-light)" opacity="0.8" />
          <circle cx="90" cy="70" r="0.2" fill="var(--steel-light)" opacity="0.4" />
          <circle cx="10" cy="80" r="0.4" fill="var(--steel-light)" opacity="0.6" />
          <circle cx="30" cy="85" r="0.2" fill="var(--steel-light)" opacity="0.7" />
          <circle cx="70" cy="90" r="0.3" fill="var(--steel-light)" opacity="0.5" />
        </svg>
      </div>

      <div className="orbit-center">
        <div className="semantic-gap">
          <span>SEMANTIC GAP</span>
          <strong>Delivered ≠ Acknowledged</strong>
          <small>no assumption crossed</small>
        </div>
      </div>

      <div className="orbit-ring-container">
        <svg viewBox="0 0 100 100" className="orbit-svg" overflow="visible">
          {/* Base orbit ring */}
          <circle 
            cx="50" cy="50" r="45" 
            fill="none" 
            stroke="var(--line-dark)" 
            strokeWidth="0.3" 
          />
          {/* Happy path (left/top part) */}
          <motion.circle 
            cx="50" cy="50" r="45" 
            fill="none" 
            stroke="var(--cyan)" 
            strokeWidth="0.4" 
            strokeDasharray="4 4"
            initial={{ pathLength: 0 }}
            animate={{ pathLength: 1 }}
            transition={{ duration: 10, ease: "linear", repeat: Infinity }}
          />
          {/* Semantic gap path (right part - dashed, distinct from the solid cyan happy path) */}
          <motion.path
            d="M 95 50 A 45 45 0 0 1 50 95"
            fill="none"
            stroke="var(--text)"
            strokeWidth="0.4" 
            strokeDasharray="2 6"
            initial={{ opacity: 0.2 }}
            animate={{ opacity: 1 }}
            transition={{ duration: 2, repeatType: "reverse", repeat: Infinity }}
          />
        </svg>

        {/* Nodes on orbit */}
        <div className="orbit-node orbit-node--requested">
          <i />
          <div className="node-label">
            <strong>REQUESTED</strong>
            <small>intent declared</small>
          </div>
        </div>
        
        <div className="orbit-node orbit-node--delivered">
          <i />
          <div className="node-label">
            <strong>DELIVERED</strong>
            <small>transport success</small>
          </div>
        </div>

        <div className="orbit-node orbit-node--acknowledged">
          <i />
          <div className="node-label">
            <strong>ACKNOWLEDGED</strong>
            <small>target reached</small>
          </div>
        </div>

        <div className="orbit-node orbit-node--running">
          <i className="is-active" />
          <div className="node-label">
            <strong>RUNNING</strong>
            <small>work in progress</small>
          </div>
        </div>

        <div className="orbit-node orbit-node--completed">
          <i />
          <div className="node-label node-label--left">
            <strong>COMPLETED</strong>
            <small>result committed</small>
          </div>
        </div>
      </div>

      <div className="orbit-legend">
        <div className="orbit-legend-items">
          <span><i className="legend-dot legend-dot--checkpoint" /> lifecycle checkpoint</span>
          <span><i className="legend-line legend-line--happy" /> happy path</span>
          <span><i className="legend-line legend-line--alternate" /> alternate path</span>
          <span><i className="legend-line legend-line--evidence" /> evidence particle</span>
          <span><i className="legend-line legend-line--gap" /> semantic gap</span>
        </div>
        <button type="button" className="orbit-replay">
          REPLAY ↻
        </button>
      </div>
    </div>
  );
}
