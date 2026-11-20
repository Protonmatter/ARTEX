import packageJson from "../../package.json";

import { t } from "@/i18n";

const currentYear = new Date().getFullYear();

export const APP_CONFIG = {
  name: "ReconRivet",
  version: packageJson.version,
  copyright: `© ${currentYear}, ReconRivet.`,
  meta: {
    title: t("ReconRivet — Autonomous Penetration Testing Console"),
    description: t("Console for the LLM-driven autonomous penetration testing system"),
  },
};
