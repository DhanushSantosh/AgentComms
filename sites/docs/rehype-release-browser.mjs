// Preserve the canonical Markdown archive for raw/agent views; human readers
// select one release, with beta history kept in its own collapsed archive.
const text = (node) => node.value ?? (node.children ?? []).map(text).join("");
const element = (tagName, properties, children) => ({ type: "element", tagName, properties, children });
const literal = (value) => ({ type: "text", value });

function group(channel, entries) {
  const id = `${channel}-release`;
  return [
    element("div", { className: ["release-picker"] }, [
      element("label", { htmlFor: id }, [literal(channel === "beta" ? "Beta release" : "Stable release")]),
      element("select", { id, "data-release-select": "" }, entries.map((entry) =>
        element("option", { value: entry.version }, [literal(entry.title.split(" — ").slice(0, 2).join(" — "))])
      ))
    ]),
    ...entries.map((entry, index) => element("section", {
      id: `release-${entry.version.replaceAll(".", "-")}`,
      className: ["release-panel"], "data-release-panel": entry.version, hidden: index !== 0
    }, entry.nodes))
  ];
}

export default function rehypeReleaseBrowser() {
  return (tree, file) => {
    if (!String(file.path).replaceAll("\\", "/").endsWith("/releases/changelog.md")) return;
    const prefix = [], suffix = [], entries = [];
    let current;
    let finished = false;
    for (const node of tree.children) {
      if (node.type === "element" && node.tagName === "h2") {
        const title = text(node);
        const match = title.match(/^v(\d+)\.\d+\.\d+(?:-[\w.-]+)?/);
        if (match && !finished) {
          const version = match[0];
          current = { version, title, beta: match[1] === "0" || version.includes("-"), nodes: [] };
          entries.push(current);
        } else if (entries.length) {
          current = undefined;
          finished = true;
        }
      }
      if (current) current.nodes.push(node);
      else (finished ? suffix : prefix).push(node);
    }
    if (!entries.length) return;
    const stable = entries.filter((entry) => !entry.beta);
    const beta = entries.filter((entry) => entry.beta);
    const stableGroup = element("div", { "data-release-group": "stable" }, stable.length ? group("stable", stable) : [
      element("p", {}, [literal("The first stable release is being prepared. Earlier releases are available in the beta archive below.")])
    ]);
    const archive = beta.length ? [element("details", { id: "beta-archive", className: ["beta-archive"], "data-release-group": "beta" }, [
      element("summary", {}, [literal(`Beta release archive (${beta.length})`)]),
      element("p", {}, [literal("Historical beta and prerelease versions. Choose one release to read its notes.")]),
      ...group("beta", beta)
    ])] : [];
    tree.children = [...prefix, stableGroup, ...archive, ...suffix];
  };
}
