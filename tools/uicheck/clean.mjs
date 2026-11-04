// Removes a test account and everything it owns: its machines' containers,
// volumes and network, then its rows.
//
//   node clean.mjs                               the account seed.mjs made
//   node clean.mjs tmpcheck-build@example.com    another test account
import { EMAIL, removeAccount } from "./common.mjs";

const emails = process.argv.slice(2);
for (const email of emails.length ? emails : [EMAIL]) {
  console.log(removeAccount(email));
}
