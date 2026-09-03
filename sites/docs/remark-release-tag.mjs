import { visit } from "unist-util-visit";

// Substitutes {{LATEST_TAG}} (e.g. "v0.6.0") and {{LATEST_VERSION}} (e.g.
// "0.6.0") in plain text, inline code, and fenced code blocks with the same
// release tag astro.config.mjs already resolves for the site's own
// PUBLIC_PRODUCT_VERSION -- so install.md's copy-pasteable commands show the
// actual latest release instead of a "replace this yourself" placeholder,
// without giving markdown content collections a live JS-expression escape
// hatch the way converting to MDX would.
export default function remarkReleaseTag({ tag, version }) {
  const replacements = [
    ["{{LATEST_TAG}}", tag],
    ["{{LATEST_VERSION}}", version]
  ];
  return (tree) => {
    visit(tree, ["text", "inlineCode", "code"], (node) => {
      if (typeof node.value !== "string") return;
      for (const [token, value] of replacements) {
        if (node.value.includes(token)) {
          node.value = node.value.split(token).join(value);
        }
      }
    });
  };
}
