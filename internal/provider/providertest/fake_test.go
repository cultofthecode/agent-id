package providertest

import (
	"testing"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

var _ provider.Provider = (*Fake)(nil)

func TestMain(m *testing.M) { testenv.Main(m) }

func TestFakeMeetsTheContract(t *testing.T) {
	RunContract(t, func(*testing.T) Subject { return NewFake().Subject() })
}

func TestFakeRecordsItsTokens(t *testing.T) {
	testenv.Isolate(t)
	fake := NewFake()
	token := mustToken(t, fake.Subject())
	if tokens := fake.Tokens(); len(tokens) != 1 || tokens[0] != token {
		t.Errorf("Tokens() does not hold the one issued token")
	}
}
