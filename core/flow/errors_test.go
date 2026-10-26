package flow

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestClassifyLogin(t *testing.T) {
	cases := []struct {
		err  error
		want Kind
	}{
		{nil, KindNone},
		{status.Error(codes.PermissionDenied, "invalid setup key"), KindLinkExpired},
		{status.Error(codes.PermissionDenied, "couldn't add peer: setup key is invalid"), KindLinkExpired},
		// служба сама отвечает PermissionDenied на проверки прав — это не ссылка
		{status.Error(codes.PermissionDenied, "not allowed: enabling ssh"), KindUnknown},
		{status.Error(codes.Unauthenticated, "token is invalid"), KindUnknown},
		{status.Error(codes.Unknown, "peer was originally enrolled via SSO"), KindNeedsBrowserLogin},
		{status.Error(codes.InvalidArgument, "invalid setup-key format"), KindLinkBroken},
		{status.Error(codes.Unknown, "couldn't add peer"), KindLinkExpired},
		{status.Error(codes.Unknown, "setup key is expired"), KindLinkExpired},
		{status.Error(codes.Unauthenticated, "peer login has expired"), KindLinkExpired},
		{status.Error(codes.NotFound, "setup-key not found"), KindLinkExpired},
		// Синтетические случаи «нет интернета»: настоящая служба набирает management с
		// grpc.WithBlock без WithReturnConnectionError, поэтому на деле отдаёт лишь
		// «context deadline exceeded». Живое «нет интернета» распознаёт Config.Online.
		{status.Error(codes.Unavailable, "dial tcp: connect: network is unreachable"), KindNoInternet},
		{status.Error(codes.Unknown, "dial tcp 1.1.1.1:443: connectex: A socket operation was attempted to an unreachable network."), KindNoInternet},
		{status.Error(codes.Unknown, "dial tcp 1.2.3.4:443: connect: no route to host"), KindNoInternet},
		{status.Error(codes.DeadlineExceeded, "context deadline exceeded"), KindServerUnreachable},
		{status.Error(codes.Unavailable, "connection refused"), KindServerUnreachable},
		{status.Error(codes.Unknown, "lookup mgmt.invalid: no such host"), KindServerUnreachable},
		{status.Error(codes.Internal, "something odd"), KindUnknown},
		{errors.New("plain i/o timeout"), KindServerUnreachable},
	}
	for _, c := range cases {
		if got := classifyLogin(c.err); got != c.want {
			t.Errorf("classifyLogin(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestKindStringNeverPanics(t *testing.T) {
	if KindNeedsLink.String() != "NeedsLink" || KindOtherBox.String() != "OtherBox" {
		t.Fatal(KindNeedsLink.String(), KindOtherBox.String())
	}
	if got := Kind(99).String(); got != "Kind(99)" {
		t.Fatal(got)
	}
	if got := Kind(-1).String(); got != "Kind(-1)" {
		t.Fatal(got)
	}
}

func TestKindOfUnwrapsFlowError(t *testing.T) {
	err := fmt.Errorf("обёртка: %w", &Error{Kind: KindOtherVPN, Detail: "utun4"})
	if KindOf(err) != KindOtherVPN {
		t.Fatal(KindOf(err))
	}
	if KindOf(errors.New("x")) != KindUnknown || KindOf(nil) != KindNone {
		t.Fatal("KindOf fallbacks")
	}
}
