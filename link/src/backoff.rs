//! How long to wait before dialling the control plane again.

use std::time::Duration;

/// The wait starts here and doubles with every failure in a row.
const MIN: Duration = Duration::from_secs(1);
/// And is never longer than this.
const MAX: Duration = Duration::from_secs(30);
/// A connection that lasted longer than this was a good one: the next wait
/// starts again from the shortest.
const STABLE_AFTER: Duration = Duration::from_secs(60);

/// The same policy guestd follows for its tunnel. A server that restarts is
/// dialled again within a second or so; one that is down for a long time is
/// tried at most every thirty seconds.
pub struct Backoff {
    next: Duration,
}

impl Backoff {
    pub fn new() -> Backoff {
        Backoff { next: MIN }
    }

    /// The wait before the next attempt, given how long the connection that
    /// has just ended lasted (or the attempt at one).
    pub fn after(&mut self, lasted: Duration) -> Duration {
        if lasted > STABLE_AFTER {
            self.next = MIN;
        }

        let wait = self.next;
        self.next = (self.next * 2).min(MAX);

        wait
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const AT_ONCE: Duration = Duration::ZERO;

    #[test]
    fn doubles_up_to_the_longest() {
        let mut backoff = Backoff::new();
        let waits: Vec<u64> = (0..8).map(|_| backoff.after(AT_ONCE).as_secs()).collect();

        assert_eq!(waits, [1, 2, 4, 8, 16, 30, 30, 30]);
    }

    #[test]
    fn starts_again_after_a_connection_that_lasted() {
        let mut backoff = Backoff::new();
        for _ in 0..6 {
            backoff.after(AT_ONCE);
        }

        assert_eq!(backoff.after(Duration::from_secs(61)), MIN);
        // And doubles from there again.
        assert_eq!(backoff.after(AT_ONCE), Duration::from_secs(2));
    }

    #[test]
    fn a_short_connection_does_not_start_it_again() {
        let mut backoff = Backoff::new();
        backoff.after(AT_ONCE);
        backoff.after(AT_ONCE);

        assert_eq!(
            backoff.after(Duration::from_secs(59)),
            Duration::from_secs(4)
        );
    }
}
