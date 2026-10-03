export type Release = {
  version: string;
  channel: "STABLE";
  name: string;
  date: string;
  dateLabel: string;
  highlights: readonly string[];
};

// Published stable releases only. Add a release during promotion; beta history
// is preserved in docs/site/releases/changelog.md and is not bundled here.
export const releases: readonly Release[] = [];
