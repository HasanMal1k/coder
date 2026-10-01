// Package guestapi is the protocol between the control plane and guestd, the
// daemon inside each machine. The .pb.go files are generated from guest.proto;
// after editing it, run `make proto`.
package guestapi

//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative guest.proto
