import GithubSlugger from "github-slugger";

// Preserve the canonical Markdown archive for raw/agent views. Human readers
// see the current stable release as an open entry and every earlier release as
// a row they can expand in place; nothing in the archive hides the current one.
const text = (node) => node.value ?? (node.children ?? []).map(text).join("");
const element = (tagName, properties, children) => ({ type: "element", tagName, properties, children });
const literal = (value) => ({ type: "text", value });
const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

// "v1.0.0 — “Pilot” — Stable — 2026-10-04" -> its parts. Unknown shapes keep
// the full title as the name rather than failing the build.
function parseTitle(title) {
  const [version, ...rest] = title.split(" — ");
  const date = rest.length && /^\d{4}-\d{2}-\d{2}$/.test(rest.at(-1)) ? rest.pop() : undefined;
  if (rest.length > 1 && /^(stable|beta|preview|rc)$/i.test(rest.at(-1))) rest.pop();
  const name = rest.join(" — ").replace(/^["“”']+|["“”']+$/g, "");
  return { version, name, date };
}

function dateLabel(date) {
  const [year, month, day] = date.split("-").map(Number);
  return `${day} ${months[month - 1]} ${year}`;
}

// A paragraph that is only a bold label ("**Breaking**") becomes a section
// label; everything else in the notes is left as written.
function labelSections(nodes) {
  return nodes.map((node) => {
    const children = (node.children ?? []).filter((child) => !(child.type === "text" && !child.value.trim()));
    if (node.tagName === "p" && children.length === 1 && children[0].tagName === "strong") {
      const label = text(children[0]).trim();
      return element("p", { className: ["release-label"], "data-kind": label.toLowerCase() }, [literal(label)]);
    }
    return node;
  });
}

function heading(entry, tagName = "h2") {
  const { version, name } = entry;
  return element(tagName, { id: entry.id, className: ["release-title"] }, [
    element("span", { className: ["release-title__version"] }, [literal(version)]),
    ...(name ? [element("span", { className: ["release-title__name"] }, [literal(name)])] : [])
  ]);
}

function meta(entry) {
  return [
    element("span", { className: ["release-badge"], "data-channel": entry.beta ? "beta" : "stable" }, [literal(entry.beta ? "Beta" : "Stable")]),
    ...(entry.date ? [element("time", { className: ["release-date"], dateTime: entry.date }, [literal(dateLabel(entry.date))])] : [])
  ];
}

function current(entry) {
  return element("article", { className: ["release-current"], "data-release-panel": entry.version }, [
    element("header", { className: ["release-current__head"] }, [
      element("p", { className: ["release-eyebrow"] }, [literal("Current release")]),
      heading(entry),
      element("div", { className: ["release-meta"] }, meta(entry))
    ]),
    element("div", { className: ["release-notes"] }, labelSections(entry.nodes))
  ]);
}

// Rows share one details name, so opening a release closes the previous one.
function row(entry) {
  return element("details", { className: ["release-row"], name: "release-history", "data-release-panel": entry.version }, [
    element("summary", { className: ["release-row__summary"] }, [heading(entry, "h3"), element("span", { className: ["release-meta"] }, meta(entry))]),
    element("div", { className: ["release-notes"] }, labelSections(entry.nodes))
  ]);
}

function list(id, title, count, intro, entries, extra = []) {
  return element("section", { className: ["release-list"], "aria-labelledby": id }, [
    element("div", { className: ["release-list__head"] }, [
      element("h2", { id, className: ["release-list__title"] }, [literal(title)]),
      element("span", { className: ["release-list__count"] }, [literal(`${count} ${count === 1 ? "release" : "releases"}`)])
    ]),
    element("p", { className: ["release-list__intro"] }, [literal(intro)]),
    element("div", { className: ["release-list__rows"] }, entries.map(row)),
    ...extra
  ]);
}

export default function rehypeReleaseBrowser() {
  return (tree, file) => {
    if (!String(file.path).replaceAll("\\", "/").endsWith("/releases/changelog.md")) return;
    const prefix = [], suffix = [], entries = [];
    const slugger = new GithubSlugger();
    let currentEntry;
    let finished = false;
    for (const node of tree.children) {
      if (node.type === "element" && node.tagName === "h2") {
        const title = text(node);
        const match = title.match(/^v(\d+)\.\d+\.\d+(?:-[\w.-]+)?/);
        if (match && !finished) {
          const version = match[0];
          // Keep the ID the plain Markdown heading always had, so existing
          // links to a release still resolve.
          currentEntry = { ...parseTitle(title), version, id: slugger.slug(title), beta: match[1] === "0" || version.includes("-"), nodes: [] };
          entries.push(currentEntry);
          continue;
        } else if (entries.length) {
          currentEntry = undefined;
          finished = true;
        }
      }
      if (currentEntry) currentEntry.nodes.push(node);
      else (finished ? suffix : prefix).push(node);
    }
    if (!entries.length) return;
    const stable = entries.filter((entry) => !entry.beta);
    const beta = entries.filter((entry) => entry.beta);
    const body = [];
    if (stable.length) body.push(element("section", { id: "current-release", className: ["release-group"], "data-release-group": "stable" }, [current(stable[0])]));
    else body.push(element("p", { className: ["release-pending"] }, [literal("The first stable release is being prepared. Earlier releases are available in the beta archive below.")]));
    if (stable.length > 1) {
      body.push(list("earlier-stable-releases", "Earlier stable releases", stable.length - 1,
        "Previous stable versions. Open a release to read its notes.", stable.slice(1)));
    }
    if (beta.length) {
      body.push(list("beta-archive", "Beta archive", beta.length,
        "Historical beta and prerelease versions, newest first. Open a release to read its notes; beta releases may contain breaking changes between minor versions.",
        beta, [element("p", { className: ["release-list__raw"] }, [element("a", { href: "/raw/releases/changelog.md" }, [literal("Read the complete archive as Markdown ↗")])])]));
    }
    tree.children = [...prefix, ...body, ...suffix];
  };
}
