"use client";

import { motion } from "framer-motion";
import "./CollisionLab.css";

export function CollisionLab() {
  return (
    <div className="collision-lab-container">
      {/* Left Scenario: Without Coordination */}
      <div className="collision-scenario">
        <div className="scenario-header">
          <strong>WITHOUT COORDINATION</strong>
          <small>overlap → conflict → delay</small>
        </div>
        
        <div className="scenario-canvas">
          <div className="time-axis">
            <span>T+00:00</span>
            <span>T+18:42</span>
          </div>

          {/* Reviewer Track */}
          <div className="agent-track track-pos-1">
            <div className="track-agent-info">
              <span className="track-agent track-agent--reviewer">REVIEWER</span>
              <small className="track-file">auth/session.ts<br/>changes</small>
            </div>
            
            <motion.div 
              className="track-progress bg-lilac"
              initial={{ width: 0 }}
              whileInView={{ width: '85%' }}
              transition={{ duration: 2, ease: "linear" }}
              viewport={{ once: true }}
            >
              <div className="progress-code">{"func verify() { return true }"}</div>
            </motion.div>
          </div>

          {/* Developer Track */}
          <div className="agent-track track-pos-2">
            <div className="track-agent-info">
              <span className="track-agent track-agent--developer">DEVELOPER</span>
              <small className="track-file">auth/session.ts<br/>changes</small>
            </div>
            
            <motion.div 
              className="track-progress bg-cyan"
              initial={{ width: 0 }}
              whileInView={{ width: '85%' }}
              transition={{ duration: 2, ease: "linear", delay: 0.5 }}
              viewport={{ once: true }}
            >
              <div className="progress-code">{"func verify() { return false }"}</div>
            </motion.div>
          </div>

          {/* Conflict Marker */}
          <motion.div 
            className="conflict-marker"
            initial={{ opacity: 0, scale: 0.8 }}
            whileInView={{ opacity: 1, scale: 1 }}
            transition={{ delay: 2.2 }}
            viewport={{ once: true }}
          >
            <div className="conflict-line" />
            <i className="conflict-icon">⚠</i>
            <strong className="conflict-text">CONFLICT DETECTED<br/>T+19:07</strong>
          </motion.div>
        </div>

        <div className="scenario-footer text-coral">
          Late conflict. Time lost.
        </div>
      </div>

      {/* Right Scenario: With Agent Comms */}
      <div className="collision-scenario">
        <div className="scenario-header">
          <strong className="text-cyan">WITH AGENT COMMS</strong>
          <small>scope lease → precise next task</small>
        </div>
        
        <div className="scenario-canvas">
          <div className="time-axis">
            <span>T+00:00</span>
            <span>T+01:12</span>
          </div>

          {/* Reviewer Track */}
          <div className="agent-track track-pos-1">
            <div className="track-agent-info">
              <span className="track-agent track-agent--reviewer">REVIEWER</span>
              <small className="track-file">auth/session.ts<br/>changes</small>
            </div>
            
            <motion.div 
              className="track-progress bg-lilac w-full"
              initial={{ width: 0 }}
              whileInView={{ width: '100%' }}
              transition={{ duration: 2.5, ease: "linear" }}
              viewport={{ once: true }}
            >
              <div className="progress-code">{"func verify() { return true }"}</div>
            </motion.div>

            {/* Scope Lease */}
            <motion.div 
              className="scope-lease-modal"
              initial={{ opacity: 0, y: 10 }}
              whileInView={{ opacity: 1, y: 0 }}
              transition={{ delay: 0.8 }}
              viewport={{ once: true }}
            >
              <i className="lock-icon">🔒</i>
              <div className="lease-details">
                <span className="text-cyan">SCOPE LEASE GRANTED</span>
                <strong>auth/session</strong>
                <small>REVIEWER · T+00:12</small>
              </div>
            </motion.div>
          </div>

          {/* Developer Track */}
          <div className="agent-track track-pos-2">
            <div className="track-agent-info">
              <span className="track-agent track-agent--developer">DEVELOPER</span>
              <small className="track-file">test/auth.ts<br/>changes</small>
            </div>
            
            <motion.div 
              className="track-progress bg-cyan w-full"
              initial={{ width: 0 }}
              whileInView={{ width: '100%' }}
              transition={{ duration: 2.5, ease: "linear", delay: 1 }}
              viewport={{ once: true }}
            >
              <div className="progress-code">{"func testVerify() { ... }"}</div>
            </motion.div>

            {/* Divert Modal */}
            <motion.div 
              className="divert-modal"
              initial={{ opacity: 0, y: 10 }}
              whileInView={{ opacity: 1, y: 0 }}
              transition={{ delay: 1.2 }}
              viewport={{ once: true }}
            >
              <i className="compass-icon">🧭</i>
              <div className="lease-details">
                <span className="text-steel">ALTERNATE TASK ASSIGNED</span>
                <strong>test/auth</strong>
                <small>DEVELOPER · T+00:13</small>
              </div>
            </motion.div>
          </div>
        </div>

        <div className="scenario-footer text-cyan">
          No collision. Continuous progress.
        </div>
      </div>
    </div>
  );
}
