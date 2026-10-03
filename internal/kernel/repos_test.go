package kernel_test

import (
	"errors"
	"testing"

	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/kernel/kerneltest"
)

// The same path is the same repo; a clone found elsewhere while its record is
// missing is that repo moved; two clones that coexist get names a person can
// tell apart.
func TestRepoIdentity(t *testing.T) {
	k := kerneltest.New(t)
	a, created, err := k.RegisterRepo(ctx, kernel.RepoFacts{Path: "/x/blog", Name: "blog", Remote: "github.com/me/blog"}, "test")
	if err != nil || !created {
		t.Fatalf("register: %v %v", created, err)
	}
	again, created := m2(k.RegisterRepo(ctx, kernel.RepoFacts{Path: "/x/blog", Name: "blog", Remote: "github.com/me/blog"}, "test"))
	if created || again.ID != a.ID {
		t.Fatal("the same path is the same repo")
	}
	clone, created := m2(k.RegisterRepo(ctx, kernel.RepoFacts{Path: "/y/blog", Name: "blog", Remote: "github.com/me/blog"}, "test"))
	if !created || clone.Name != "blog-2" {
		t.Fatalf("a second clone that coexists is a second place: %+v", clone)
	}
	if _, changed, err := k.SetRepoStatus(ctx, a.ID, kernel.RepoMissing, "test"); err != nil || !changed {
		t.Fatal(err)
	}
	if _, changed, _ := k.SetRepoStatus(ctx, a.ID, kernel.RepoMissing, "test"); changed {
		t.Fatal("no change, no fact")
	}
	moved, created := m2(k.RegisterRepo(ctx, kernel.RepoFacts{Path: "/z/blog", Name: "blog", Remote: "github.com/me/blog"}, "test"))
	if created || moved.ID != a.ID || moved.Path != "/z/blog" || moved.Status != kernel.RepoPresent {
		t.Fatalf("a missing repo found elsewhere keeps its id: %+v", moved)
	}
	kinds := map[string]int{}
	for _, e := range m(k.ListEvents(ctx, kernel.ListFilter{})) {
		kinds[e.Kind]++
	}
	if kinds["repo.registered"] != 2 || kinds["repo.missing"] != 1 || kinds["repo.relocated"] != 1 {
		t.Fatalf("facts: %v", kinds)
	}
	if _, err := k.RelocateRepo(ctx, a.ID, "/y/blog", "", "", "test"); !errors.Is(err, kernel.ErrPathTaken) {
		t.Fatalf("two repos never share a path: %v", err)
	}
}

// One writer per original directory; a repo in use cannot be forgotten.
func TestInPlaceLeaseAndForgetting(t *testing.T) {
	k := kerneltest.New(t)
	r, _ := m2(k.RegisterRepo(ctx, kernel.RepoFacts{Path: "/x/notes", Name: "notes"}, "test"))
	a := m(k.CreateWorkItem(ctx, "a", "test", kernel.CreateWorkItemOpts{}))
	b := m(k.CreateWorkItem(ctx, "b", "test", kernel.CreateWorkItemOpts{}))
	ca := m(k.CreateCheckout(ctx, kernel.Checkout{WorkItemID: a.ID, RepoID: r.ID, Mode: kernel.CheckoutInPlace, Path: r.Path}, r.Name, "test"))
	var lease *kernel.LeaseError
	if _, err := k.CreateCheckout(ctx, kernel.Checkout{WorkItemID: b.ID, RepoID: r.ID, Mode: kernel.CheckoutInPlace, Path: r.Path}, r.Name, "test"); !errors.As(err, &lease) || lease.Holder != a.ID {
		t.Fatalf("lease: %v", err)
	}
	if err := k.ForgetRepo(ctx, r.ID, "test"); !errors.Is(err, kernel.ErrRepoInUse) {
		t.Fatalf("in use: %v", err)
	}
	m(k.ArchiveCheckout(ctx, ca.ID, "test", kernel.Payload{"released": true}))
	m(k.CreateCheckout(ctx, kernel.Checkout{WorkItemID: b.ID, RepoID: r.ID, Mode: kernel.CheckoutInPlace, Path: r.Path}, r.Name, "test"))
	created := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "checkout.created"}))
	if len(created) != 2 || created[0].WorkItemID != a.ID || created[0].ThreadID != a.ThreadID {
		t.Fatalf("a checkout is a fact on its work item's thread: %+v", created)
	}
}
