import packageJson from "../../package.json";

const currentYear = new Date().getFullYear();

export const APP_CONFIG = {
  name: "Elenxa",
  version: packageJson.version,
  copyright: `© ${currentYear}, Elenxa.`,
  meta: {
    title: "Elenxa — Autonomous Penetration Testing Console",
    description: "Console for an LLM-driven autonomous penetration testing system",
  },
};
