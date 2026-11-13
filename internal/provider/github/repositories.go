package github

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// Pagination of GET /installation/repositories.
const (
	repositoriesPerPage = 100
	maxRepositoryPages  = 100
)

// repositoryNamePattern is the shape of a repository owner or name that the
// adapter accepts, so that a hostile answer cannot reach a terminal with
// escape sequences.
var repositoryNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

// Repository is a repository that an installation can access.
type Repository struct {
	Owner string
	Name  string
}

// repositoriesAnswer is the part of GET /installation/repositories that the
// adapter reads.
type repositoriesAnswer struct {
	Repositories []struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repositories"`
}

// ListRepositories lists the repositories that token, an installation token, can
// access. It reads the pages of the fixed base URL in order until a page is
// not full, and never follows a Link header to another URL.
func (p *Provider) ListRepositories(ctx context.Context, token provider.Token) ([]Repository, error) {
	const op = "list the installation repositories"
	if !validToken(token.Value) {
		return nil, fmt.Errorf("github: %s: %w: no usable installation token", op, provider.ErrUnauthorized)
	}
	var repositories []Repository
	for page := 1; page <= maxRepositoryPages; page++ {
		var answer repositoriesAnswer
		path := "/installation/repositories?per_page=" + strconv.Itoa(repositoriesPerPage) + "&page=" + strconv.Itoa(page)
		if err := p.call(ctx, op, http.MethodGet, path, "Bearer "+token.Value, http.StatusOK, &answer); err != nil {
			return nil, err
		}
		for _, r := range answer.Repositories {
			if !repositoryNamePattern.MatchString(r.Owner.Login) || !repositoryNamePattern.MatchString(r.Name) {
				return nil, fmt.Errorf("github: %s: %w: %w: a repository owner or name has unexpected characters", op, provider.ErrUnavailable, errUnusableAnswer)
			}
			repositories = append(repositories, Repository{Owner: r.Owner.Login, Name: r.Name})
		}
		if len(answer.Repositories) < repositoriesPerPage {
			return repositories, nil
		}
	}
	return nil, fmt.Errorf("github: %s: %w: %w: more than %d pages", op, provider.ErrUnavailable, errUnusableAnswer, maxRepositoryPages)
}

// Repositories implements provider.Provider: the "owner/name" of each
// repository that ListRepositories returns.
func (p *Provider) Repositories(ctx context.Context, token provider.Token) ([]string, error) {
	listed, err := p.ListRepositories(ctx, token)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(listed))
	for i, r := range listed {
		names[i] = r.Owner + "/" + r.Name
	}
	return names, nil
}
