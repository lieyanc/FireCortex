import Script from "next/script";

const BANNER = String.raw`
 _____ ___ ____  _____ ____ ___  ____ _____ _______  __
|  ___|_ _|  _ \| ____/ ___/ _ \|  _ \_   _| ____\ \/ /
| |_   | || |_) |  _|| |  | | | | |_) || | |  _|  \  /
|  _|  | ||  _ <| |__| |__| |_| |  _ < | | | |___ /  \
|_|   |___|_| \_\_____\____\___/|_| \_\|_| |_____/_/\_\
`;

const BANNER_SCRIPT = `
(() => {
  const key = "__FIRECORTEX_DEVTOOLS_BANNER__";
  if (globalThis[key]) return;
  globalThis[key] = true;
  const banner = ${JSON.stringify(BANNER)};
  console.log("%c" + banner, "color:#018eee;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace;font-weight:700;line-height:1.15");
  console.log(
    "%c  %c  %c  %c  %c  %c  %c  %c  ",
    "background:#003c75",
    "background:#005cc8",
    "background:#0076d6",
    "background:#018eee",
    "background:#38b6ff",
    "background:#7dd3fc",
    "background:#bae6fd",
    "background:transparent"
  );
  console.log("%cRepository: https://github.com/lieyanc/FireCortex  |  License: Apache License 2.0", "color:#64748b;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace");
})();
`;

export function DevtoolsBrandBanner() {
  return (
    <Script id="firecortex-devtools-brand" strategy="afterInteractive">
      {BANNER_SCRIPT}
    </Script>
  );
}
