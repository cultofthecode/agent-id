package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestRepositoriesOfTheFakeApp(t *testing.T) {
	app := newFakeApp(t, "o/a", "o/b")
	p := newAdapter(t, app)
	token, err := p.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.ListRepositories(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Repository{{"o", "a"}, {"o", "b"}}; !slices.Equal(got, want) {
		t.Errorf("ListRepositories() = %v, want %v", got, want)
	}
}

func TestRepositoriesWithAnExpiredToken(t *testing.T) {
	app := newFakeApp(t, "o/a")
	app.SetTokenLifetime(-time.Minute)
	p := newAdapter(t, app)
	token, err := p.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ListRepositories(context.Background(), token); !errors.Is(err, provider.ErrUnauthorized) {
		t.Errorf("ListRepositories() with an expired token: %v, want ErrUnauthorized", err)
	}
	if _, err := p.ListRepositories(context.Background(), provider.Token{Value: "x\ny"}); !errors.Is(err, provider.ErrUnauthorized) {
		t.Errorf("ListRepositories() with an invalid token: %v, want ErrUnauthorized", err)
	}
}

func TestRepositoriesPagination(t *testing.T) {
	const total = 150
	fake, p := newHostileAPI(t)
	var pages []string
	fake.Handle("GET /installation/repositories", func(w http.ResponseWriter, r *http.Request) {
		pages = append(pages, r.URL.RawQuery)
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		w.Header().Set("Link", `<https://elsewhere.test/next>; rel="next"`)
		answer(http.StatusOK, repositoryPage(total, (page-1)*100, min(100, total-(page-1)*100)))(w, r)
	})
	got, err := p.ListRepositories(context.Background(), provider.Token{Value: testenv.FakeToken("pages")})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != total || got[149] != (Repository{"o", "r149"}) {
		t.Errorf("ListRepositories() returned %d repositories, want %d in order", len(got), total)
	}
	if want := []string{"per_page=100&page=1", "per_page=100&page=2"}; !slices.Equal(pages, want) {
		t.Errorf("requested pages %v, want %v", pages, want)
	}
}

func TestRepositoriesPageCap(t *testing.T) {
	fake, p := newHostileAPI(t)
	fake.Handle("GET /installation/repositories", answer(http.StatusOK, repositoryPage(1_000_000, 0, 100)))
	if _, err := p.ListRepositories(context.Background(), provider.Token{Value: testenv.FakeToken("cap")}); !errors.Is(err, provider.ErrUnavailable) {
		t.Errorf("ListRepositories() past the page cap: %v, want ErrUnavailable", err)
	}
}

func TestRepositoriesRejectHostileNames(t *testing.T) {
	for _, name := range []string{"", "a\x1b[2Jb", "a/b", "a b", "ą"} {
		fake, p := newHostileAPI(t)
		fake.Handle("GET /installation/repositories", answer(http.StatusOK, map[string]any{
			"total_count":  1,
			"repositories": []map[string]any{{"name": name, "owner": map[string]any{"login": "o"}}},
		}))
		got, err := p.ListRepositories(context.Background(), provider.Token{Value: testenv.FakeToken("names")})
		if !errors.Is(err, provider.ErrUnavailable) || got != nil {
			t.Errorf("ListRepositories() with name %q: %v, %v; want ErrUnavailable", name, got, err)
		}
	}
}

// repositoryPage returns a page of count repositories o/r<n> from first, of
// total.
func repositoryPage(total, first, count int) map[string]any {
	repositories := make([]map[string]any, 0, count)
	for i := first; i < first+count; i++ {
		repositories = append(repositories, map[string]any{"name": fmt.Sprintf("r%d", i), "owner": map[string]any{"login": "o"}})
	}
	return map[string]any{"total_count": total, "repositories": repositories}
}

func TestRepositoriesIgnoreAWrongTotalCount(t *testing.T) {
	fake, p := newHostileAPI(t)
	fake.Handle("GET /installation/repositories", func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		answer(http.StatusOK, repositoryPage(0, (page-1)*100, min(100, 130-(page-1)*100)))(w, r)
	})
	got, err := p.ListRepositories(context.Background(), provider.Token{Value: testenv.FakeToken("count")})
	if err != nil || len(got) != 130 {
		t.Errorf("ListRepositories() with total_count 0 = %d repositories, %v; want all 130", len(got), err)
	}
}

func TestRepositoriesListsOwnerSlashName(t *testing.T) {
	app := newFakeApp(t, "cultofthecode/widgets")
	p := newAdapter(t, app)
	token, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("Token(): %v", err)
	}
	got, err := p.Repositories(context.Background(), token)
	if err != nil || !slices.Equal(got, []string{"cultofthecode/widgets"}) {
		t.Errorf("Repositories() = %v, %v; want [cultofthecode/widgets]", got, err)
	}
	if _, err := p.Repositories(context.Background(), provider.Token{Value: "x"}); !errors.Is(err, provider.ErrUnauthorized) {
		t.Errorf("Repositories() with an invalid token: %v, want ErrUnauthorized", err)
	}
}
