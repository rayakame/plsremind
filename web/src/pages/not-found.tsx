import { Link } from "wouter";
import { useT } from "../i18n/index.js";
import { routes } from "../shell/router.js";

export function NotFound() {
  const t = useT();
  return (
    <>
      <h1 className="h1">{t("common.notFound")}</h1>
      <p className="text">
        <Link href={routes.items}>{t("nav.all")}</Link>
      </p>
    </>
  );
}
