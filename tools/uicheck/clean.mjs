// Removes the account seed.mjs made, and everything it owns, in the order
// docs/STATUS.md gives: containers, volumes and the network first (there are
// none for seeded machines, whose machine IDs are made up, but someone may
// have created a real one while signed in as it), then the rows.
import { execFileSync } from "node:child_process";
import { rmSync } from "node:fs";
import { EMAIL, psql } from "./common.mjs";

/** Runs a docker command that is allowed to find nothing to do. */
function docker(...args) {
  try {
    execFileSync("docker", args, { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

const user = psql(`select id from users where email = '${EMAIL}'`).trim();
if (!user) {
  console.log(`No account ${EMAIL}; nothing to remove.`);
} else {
  const instances = psql(`select id from instances where owner_id = '${user}'`)
    .trim()
    .split("\n")
    .filter(Boolean);

  let real = 0;
  for (const id of instances) {
    // Asked first: `docker rm -f` reports success for a name that isn't there.
    if (docker("container", "inspect", `coder-${id}`)) {
      docker("rm", "-f", `coder-${id}`);
      real++;
    }
    docker("volume", "rm", `coder-${id}`);
  }
  docker("network", "rm", `coder-user-${user}`);

  psql(
    `delete from chats where owner_id = '${user}';
     delete from instances where owner_id = '${user}';
     delete from users where id = '${user}';`,
  );
  console.log(
    `Removed ${EMAIL}: ${instances.length} machine rows` +
      (real ? `, ${real} of them with a real container` : "") +
      ", its chats, and the account.",
  );
}

// The session in it is for an account that no longer exists.
rmSync(new URL("./seed.json", import.meta.url), { force: true });
