import {
  siAngular,
  siApachekafka,
  siAstro,
  siBun,
  siCaddy,
  siClickhouse,
  siCplusplus,
  siDeno,
  siDjango,
  siDocker,
  siDotnet,
  siElasticsearch,
  siElixir,
  siExpress,
  siFastapi,
  siFlask,
  siGit,
  siGithubactions,
  siGnubash,
  siGo,
  siGraphql,
  siHtml5,
  siJavascript,
  siKotlin,
  siLaravel,
  siMariadb,
  siMongodb,
  siMysql,
  siNestjs,
  siNextdotjs,
  siNginx,
  siNodedotjs,
  siNuxt,
  siOpenjdk,
  siPhp,
  siPostgresql,
  siPython,
  siRabbitmq,
  siReact,
  siRedis,
  siRemix,
  siRuby,
  siRubyonrails,
  siRust,
  siSolid,
  siSpring,
  siSqlite,
  siSvelte,
  siSwift,
  siTailwindcss,
  siTypescript,
  siVite,
  siVuedotjs,
} from "simple-icons";
import { cn } from "@/lib/utils";

/** A technology's mark, as Simple Icons draws it: one path on a 24 by 24 square. */
export type Mark = { title: string; path: string; hex: string };

/**
 * The technologies recognised in what a component says it is made with, and
 * how each is spelled there. A design says "React + TypeScript (Vite)" or
 * "ASP.NET Core" in its own words, so these match loosely.
 */
const MARKS: [RegExp, Mark][] = [
  [/\bnext(\.?js)?\b/i, siNextdotjs],
  [/\breact\b/i, siReact],
  [/\bnuxt\b/i, siNuxt],
  [/\bvue(\.?js)?\b/i, siVuedotjs],
  [/\bsvelte(kit)?\b/i, siSvelte],
  [/\bangular\b/i, siAngular],
  [/\bsolid(\.?js)?\b/i, siSolid],
  [/\bremix\b/i, siRemix],
  [/\bastro\b/i, siAstro],
  [/\bvite\b/i, siVite],
  [/\btailwind/i, siTailwindcss],
  [/\bhtml\b/i, siHtml5],
  [/\bgo(lang)?\b/i, siGo],
  [/(\.net\b|\bdotnet\b|\bc#)/i, siDotnet],
  [/\bnest(\.?js)?\b/i, siNestjs],
  [/\bexpress\b/i, siExpress],
  [/\bnode(\.?js)?\b/i, siNodedotjs],
  [/\bdeno\b/i, siDeno],
  [/\bbun\b/i, siBun],
  [/\bfastapi\b/i, siFastapi],
  [/\bdjango\b/i, siDjango],
  [/\bflask\b/i, siFlask],
  [/\bpython\b/i, siPython],
  [/\brust\b/i, siRust],
  [/\bspring\b/i, siSpring],
  [/\b(java|jvm|openjdk)\b/i, siOpenjdk],
  [/\bkotlin\b/i, siKotlin],
  [/\bswift\b/i, siSwift],
  [/\belixir\b/i, siElixir],
  [/\blaravel\b/i, siLaravel],
  [/\bphp\b/i, siPhp],
  [/\brails\b/i, siRubyonrails],
  [/\bruby\b/i, siRuby],
  [/\bc\+\+/i, siCplusplus],
  [/\bgraphql\b/i, siGraphql],
  [/\bpostgres(ql)?\b/i, siPostgresql],
  [/\bmysql\b/i, siMysql],
  [/\bmariadb\b/i, siMariadb],
  [/\bsqlite\b/i, siSqlite],
  [/\bmongo(db)?\b/i, siMongodb],
  [/\bredis\b/i, siRedis],
  [/\bclickhouse\b/i, siClickhouse],
  [/\belastic(search)?\b/i, siElasticsearch],
  [/\brabbitmq\b/i, siRabbitmq],
  [/\bkafka\b/i, siApachekafka],
  [/\bnginx\b/i, siNginx],
  [/\bcaddy\b/i, siCaddy],
  [/\bdocker\b/i, siDocker],
  [/\bgithub actions\b/i, siGithubactions],
  [/\bgit\b/i, siGit],
  [/\b(shell|bash)\b/i, siGnubash],
  [/\btypescript\b/i, siTypescript],
  [/\bjavascript\b/i, siJavascript],
];

/**
 * The mark for what a component is made with: of everything recognised in
 * the description, the one named first. "Go + SQLite" is Go.
 */
export function techMark(tech: string): Mark | undefined {
  let found: Mark | undefined;
  let at = Infinity;

  for (const [spelled, mark] of MARKS) {
    const index = tech.search(spelled);
    if (index >= 0 && index < at) {
      found = mark;
      at = index;
    }
  }
  return found;
}

/**
 * A brand's colour as it can be shown on a dark surface. Many are fine as
 * they are. A dark one is lifted towards white until it can be seen, and
 * one that is simply black (Next.js, Rust) becomes the page's own white.
 */
function onDark(hex: string) {
  const [r, g, b] = [0, 2, 4].map((i) => parseInt(hex.slice(i, i + 2), 16));
  // How bright the eye takes it for, from 0 to 255.
  const brightness = 0.299 * r + 0.587 * g + 0.114 * b;
  if (brightness >= 125) return `#${hex}`;

  const colourful = Math.max(r, g, b) - Math.min(r, g, b) > 60;
  if (!colourful) return "var(--foreground)";

  const lift = (channel: number) => Math.round(channel + (255 - channel) * 0.4);
  return `rgb(${lift(r)} ${lift(g)} ${lift(b)})`;
}

/** A technology's mark, in its own colour where that shows on dark. */
export function TechLogo({ mark, className }: { mark: Mark; className?: string }) {
  return (
    <svg
      role="img"
      aria-label={mark.title}
      viewBox="0 0 24 24"
      className={cn("size-5 shrink-0", className)}
      fill={onDark(mark.hex)}
    >
      <path d={mark.path} />
    </svg>
  );
}
