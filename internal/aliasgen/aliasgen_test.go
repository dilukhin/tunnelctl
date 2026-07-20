package aliasgen

import "testing"

func TestGenerateYandexAliasFromKey(t *testing.T) {
	used := map[string]bool{}
	got := Generate(Input{User: "user", Host: "192.0.2.10", Key: "~/.ssh/id_yandex_vm"}, used)
	if got.Alias != "yandex" {
		t.Fatalf("ожидался алиас yandex, получено %q", got.Alias)
	}
}

func TestGenerateTunnelAliasFromGenericKeyAndUser(t *testing.T) {
	used := map[string]bool{}
	got := Generate(Input{User: "tunneluser", Host: "198.51.100.20", Key: "~/.ssh/id_ed25519"}, used)
	if got.Alias != "ip-198-51" {
		t.Fatalf("ожидался алиас ip-198-51, получено %q", got.Alias)
	}
	if got.Name != "198-51-100-20" {
		t.Fatalf("ожидалось имя 198-51-100-20, получено %q", got.Name)
	}
}

func TestGenerateUniqueAlias(t *testing.T) {
	used := map[string]bool{"yandex": true, "yandex-192": true}
	got := Generate(Input{User: "user", Host: "192.0.2.10", Key: "~/.ssh/id_yandex_vm"}, used)
	if got.Alias != "yandex-2" {
		t.Fatalf("ожидался алиас yandex-2, получено %q", got.Alias)
	}
}
