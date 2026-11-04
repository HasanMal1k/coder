// Package linkapi is the protocol of the distributed harness: what the link
// daemon on each machine (linkd, in link/) and the control plane say to each
// other. The .pb.go files are generated from link.proto; after editing it,
// run `make proto`. linkd is written in Rust and generates its own code from
// the same file when it is built.
package linkapi

//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative link.proto

// Protocol is the revision of the protocol this code speaks. Both ends say
// theirs in their hello, and a stream between two that differ is ended.
const Protocol = 1

// MaxFrame caps one frame, in bytes. Both ends refuse anything larger.
const MaxFrame = 256 << 10

// PeerPort is the port a machine's link daemon listens on for its peers, on
// the owner's private network.
const PeerPort = 7600

// Program is the name of the link daemon's executable in a machine.
const Program = "linkd"

// What linkd is told through its environment, beside the machine's own
// identity (guestapi.EnvInstanceID and guestapi.EnvGuestToken).
const (
	// EnvLinkURL is where the control plane's link gateway is, as linkd
	// dials it: "http://host:8081". A machine without it was created before
	// links existed, and runs none.
	EnvLinkURL = "CODER_LINK_URL"
)
