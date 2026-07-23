package config

import (
	"encoding/json"
	"fmt"
)

// CurrentSchemaVersion — версия формата tunnelctl.json.
const CurrentSchemaVersion = 1

type configAlias Config

type configDocument struct {
	Version int `json:"version"`
	configAlias
}

// MarshalJSON всегда сохраняет явную версию схемы конфигурации.
func (c Config) MarshalJSON() ([]byte, error) {
	return json.Marshal(configDocument{
		Version:     CurrentSchemaVersion,
		configAlias: configAlias(c),
	})
}

// UnmarshalJSON принимает legacy-конфиги без version как схему 0 и применяет миграции по порядку.
func (c *Config) UnmarshalJSON(data []byte) error {
	doc := configDocument{configAlias: configAlias(*c)}
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	if doc.Version < 0 {
		return fmt.Errorf("некорректная версия схемы конфигурации: %d", doc.Version)
	}
	if doc.Version > CurrentSchemaVersion {
		return fmt.Errorf("конфигурация имеет более новую версию схемы %d; поддерживается %d", doc.Version, CurrentSchemaVersion)
	}
	*c = Config(doc.configAlias)
	return migrate(c, doc.Version)
}

func migrate(cfg *Config, from int) error {
	for version := from; version < CurrentSchemaVersion; version++ {
		switch version {
		case 0:
			// Схема 0 — legacy-файл без поля version. Структура совместима со схемой 1.
		default:
			return fmt.Errorf("не определена миграция конфигурации с версии %d", version)
		}
	}
	return nil
}
