//! How two machines prove to each other that they belong to the same mesh.
//!
//! The control plane gives every machine of a mesh the same key. Each side
//! of a stream sends the other a nonce in its hello, and answers the other's
//! with an HMAC under that key. The key itself never crosses the wire, and
//! an answer is good for one stream, one direction and one pair of machines.

use hmac::{Hmac, Mac};
use sha2::Sha256;
use std::io::Read;

/// The length of a nonce, and the least a peer's may be.
pub const NONCE_BYTES: usize = 32;

/// Which side of the stream a proof is from. It is part of what is signed,
/// so one side's proof can never be passed off as the other's.
#[derive(Clone, Copy)]
pub enum Role {
    /// The machine that opened the stream.
    Dial,
    /// The machine that was dialled.
    Accept,
}

/// What is signed: who is proving, to whom, and for which challenge.
pub struct Claim<'a> {
    pub role: Role,
    /// The machine proving itself, and the machine it proves itself to.
    pub from: &'a str,
    pub to: &'a str,
    pub mesh: &'a str,
    /// The challenge: the nonce the other side sent.
    pub nonce: &'a [u8],
}

fn signer(key: &[u8], claim: &Claim) -> Hmac<Sha256> {
    // HMAC takes a key of any length, so this cannot fail.
    let mut mac = Hmac::<Sha256>::new_from_slice(key).expect("HMAC takes any key");

    mac.update(b"coder-link-proof-v1");
    mac.update(match claim.role {
        Role::Dial => b"dial",
        Role::Accept => b"accp",
    });
    // Each part with its length in front, so that no two claims can be
    // made of the same bytes cut differently.
    for part in [
        claim.from.as_bytes(),
        claim.to.as_bytes(),
        claim.mesh.as_bytes(),
        claim.nonce,
    ] {
        mac.update(&(part.len() as u64).to_be_bytes());
        mac.update(part);
    }

    mac
}

/// The proof of a claim under a mesh's key.
pub fn prove(key: &[u8], claim: &Claim) -> Vec<u8> {
    signer(key, claim).finalize().into_bytes().to_vec()
}

/// Whether a proof is the proof of a claim under a mesh's key. It takes the
/// same time whatever is wrong with it.
pub fn holds(key: &[u8], claim: &Claim, proof: &[u8]) -> bool {
    signer(key, claim).verify_slice(proof).is_ok()
}

/// A fresh challenge, from the system's source of randomness.
pub fn nonce() -> std::io::Result<Vec<u8>> {
    let mut bytes = vec![0u8; NONCE_BYTES];
    std::fs::File::open("/dev/urandom")?.read_exact(&mut bytes)?;

    Ok(bytes)
}

#[cfg(test)]
mod tests {
    use super::*;

    const KEY: &[u8] = b"the mesh's key, thirty-two bytes";

    fn claim<'a>(nonce: &'a [u8]) -> Claim<'a> {
        Claim {
            role: Role::Dial,
            from: "machine-a",
            to: "machine-b",
            mesh: "chat-1",
            nonce,
        }
    }

    #[test]
    fn a_proof_holds_for_its_own_claim() {
        let proof = prove(KEY, &claim(b"nonce"));
        assert!(holds(KEY, &claim(b"nonce"), &proof));
    }

    #[test]
    fn it_holds_for_nothing_else() {
        let proof = prove(KEY, &claim(b"nonce"));

        assert!(!holds(b"another mesh's key", &claim(b"nonce"), &proof));
        assert!(!holds(KEY, &claim(b"another nonce"), &proof));
        assert!(!holds(
            KEY,
            &Claim {
                role: Role::Accept,
                ..claim(b"nonce")
            },
            &proof
        ));
        assert!(!holds(
            KEY,
            &Claim {
                from: "machine-c",
                ..claim(b"nonce")
            },
            &proof
        ));
        assert!(!holds(
            KEY,
            &Claim {
                to: "machine-c",
                ..claim(b"nonce")
            },
            &proof
        ));
        assert!(!holds(
            KEY,
            &Claim {
                mesh: "chat-2",
                ..claim(b"nonce")
            },
            &proof
        ));
        assert!(!holds(KEY, &claim(b"nonce"), &proof[..proof.len() - 1]));
        assert!(!holds(KEY, &claim(b"nonce"), b""));
    }

    #[test]
    fn parts_cannot_be_cut_differently_to_mean_the_same() {
        let one = Claim {
            from: "ab",
            to: "c",
            ..claim(b"n")
        };
        let other = Claim {
            from: "a",
            to: "bc",
            ..claim(b"n")
        };

        assert_ne!(prove(KEY, &one), prove(KEY, &other));
    }

    #[test]
    fn nonces_are_long_and_differ() {
        let (one, other) = (nonce().unwrap(), nonce().unwrap());

        assert_eq!(one.len(), NONCE_BYTES);
        assert_ne!(one, other);
    }
}
