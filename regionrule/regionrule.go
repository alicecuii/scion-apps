package regionrule

import (
	"fmt"
	"gopkg.in/yaml.v2"
	"io"
	"log"
	"os"
)

type Rule struct {
	Name       string `yaml:"name"`
	ISD        int    `yaml:"ISD"`
	Preference string `yaml:"Preference"`
}

type AppConfig struct {
	Apps []struct {
		Name  string `yaml:"name"`
		Rules []Rule `yaml:"rules"`
	} `yaml:"apps"`
}

func GetPreferences(filePath string) ([]string, error) {
	// Open the YAML file
	file, err := os.Open("app.yml")
	if err != nil {
		log.Fatalf("Error opening YAML file: %v", err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Fatalf("Error closing YAML file: %v", err)
		}
	}(file)

	// Read the YAML data from the file
	yamlData, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("Error reading YAML file: %v", err)
	}

	// Unmarshal the YAML data into the AppConfig struct
	var config AppConfig
	err = yaml.Unmarshal(yamlData, &config)
	if err != nil {
		log.Fatalf("Error unmarshaling YAML: %v", err)
	}

	// Print the parsed configuration to the screen
	fmt.Printf("Parsed YAML Configuration:\n")
	var preferences []string
	for _, app := range config.Apps {
		for _, rule := range app.Rules {
			preferences = append(preferences, rule.Preference)
		}
	}

	return preferences, nil
}
