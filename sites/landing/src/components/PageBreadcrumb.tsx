import Link from "next/link";

type PageBreadcrumbProperties = {
  label: string;
};

export function PageBreadcrumb({ label }: PageBreadcrumbProperties) {
  return (
    <div className="page-breadcrumb">
      <button type="button" data-back-button>
        <span aria-hidden="true">←</span> Back
      </button>
      <nav aria-label="Breadcrumb">
        <ol>
          <li><Link href="/">Home</Link></li>
          <li aria-current="page">{label}</li>
        </ol>
      </nav>
    </div>
  );
}
