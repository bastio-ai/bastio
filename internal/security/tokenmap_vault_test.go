package security

import (
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/pkg/encryption"
)

func TestBatchTokenVaultAuthenticatedContext(t *testing.T) {
	cipher, err := encryption.New("")
	if err != nil {
		t.Fatal(err)
	}
	tm := NewRequestTokenMap(TokenStyleCurly)
	token := tm.Add("email", "person@example.test")
	vault, err := tm.Seal(cipher, "tenant", "scope/manifest/item")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(vault.Ciphertext, "person") {
		t.Fatal("plaintext vault")
	}
	for _, wrong := range []struct{ tenant, context string }{{"other", "scope/manifest/item"}, {"tenant", "other/manifest/item"}, {"tenant", "scope/manifest/other"}} {
		if _, err = OpenTokenMap(cipher, vault, wrong.tenant, wrong.context); err == nil {
			t.Fatal("cross-context vault opened")
		}
	}
	opened, err := OpenTokenMap(cipher, vault, "tenant", "scope/manifest/item")
	if err != nil {
		t.Fatal(err)
	}
	if value, n := opened.Restore(token); value != "person@example.test" || n != 1 {
		t.Fatal("vault did not restore")
	}
	if _, err = tm.Seal(nil, "tenant", "scope"); err == nil {
		t.Fatal("unencrypted vault accepted")
	}
}
