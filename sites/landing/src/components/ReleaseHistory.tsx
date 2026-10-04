"use client";

import { useState } from "react";
import type { Release } from "@/lib/releases";
import styles from "@/app/releases/releases.module.css";

export function ReleaseHistory({ releases }: { releases: readonly Release[] }) {
  const [version, setVersion] = useState(releases[0]?.version ?? "");
  const release = releases.find((entry) => entry.version === version);

  if (!release) {
    return <div className={styles.empty}>
      <h2>The first stable release is being prepared.</h2>
      <p>Published stable releases will appear here. The complete beta history is preserved in the documentation.</p>
    </div>;
  }

  return <>
    <div className={styles.selector}>
      <label htmlFor="stable-release">Release</label>
      <select id="stable-release" value={version} onChange={(event) => setVersion(event.target.value)}>
        {releases.map((entry) => <option key={entry.version} value={entry.version}>{entry.version} — {entry.name}</option>)}
      </select>
    </div>
    <article className={styles.selected} aria-live="polite" aria-atomic="true">
      <div className="release-head">
        <h2 className="release-version">{release.version}</h2>
        <span className="release-channel">Stable</span>
        <span className="release-name">{release.name}</span>
        <time className="release-date" dateTime={release.date}>{release.dateLabel}</time>
      </div>
      <ul className="release-highlights">
        {release.highlights.map((highlight) => <li key={highlight}>{highlight}</li>)}
      </ul>
    </article>
  </>;
}
