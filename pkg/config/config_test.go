package config_test

import (
	"os"
	"testing"

	"github.com/kwilteam/kwil-db/pkg/config"
	"github.com/kwilteam/kwil-db/pkg/crypto"

	"github.com/cstockton/go-conv"
)

var (
	envPrefix = "KWIL_TEST"
)

func Test_Config(t *testing.T) {
	// TEST 1: load config with no private key, should fail
	testCfg := &TestConfig{
		Inner: InnerTestConfig{
			Val1: "val1",
		},
	}

	err := config.LoadConfig(RegisteredVariables, envPrefix, testCfg)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	os.Setenv("KWIL_TEST_PRIVATE_KEY", "f2d82d73ba03a7e843443f2b3179a01398144baa4a23d40d1e8a3a8e4fb217d0484d59f4de46b2174ebce66ac3afa7989b444244323c19a74b683f54cf33227c")

	// TEST 2: load config with private key, should succeed
	err = config.LoadConfig(RegisteredVariables, envPrefix, testCfg)
	if err != nil {
		t.Fatal(err)
	}

	if testCfg.PrivateKey == nil {
		t.Fatal("PrivateKey is nil")
	}

	if testCfg.Inner.Val1 != "val1" {
		t.Fatal("Inner.Val1 is not val1")
	}

	if testCfg.Inner.Val2 != 1 {
		t.Fatal("Inner.Val2 is not 1")
	}

	// TEST 3: load config with config value overriding default but NOT env value, should succeed
	testCfg.Inner.Val2 = 2
	err = config.LoadConfig(RegisteredVariables, envPrefix, testCfg)
	if err != nil {
		t.Fatal(err)
	}

	if testCfg.PrivateKey == nil {
		t.Fatal("PrivateKey is nil")
	}

	if testCfg.Inner.Val1 != "val1" {
		t.Fatal("Inner.Val1 is not val1")
	}

	if testCfg.Inner.Val2 != 2 {
		t.Fatal("Inner.Val2 is not 2")
	}

	// TEST 4: load config with env value overriding cfg value AND default, should succeed
	// set innerval1 to "newval1"
	os.Setenv("KWIL_TEST_INNER_VAL_1", "newval1")

	// reload config
	err = config.LoadConfig(RegisteredVariables, envPrefix, testCfg)
	if err != nil {
		t.Fatal(err)
	}

	// if there is an env set, it should override the config file
	if testCfg.Inner.Val1 != "newval1" {
		t.Fatal("Inner.Val1 is not newval1")
	}
}

func Test_Failures(t *testing.T) {
	// TEST 1: load config with no private key, should fail
	testCfg := &TestConfig{
		Inner: InnerTestConfig{
			Val1: "val1",
		},
	}

	invalidVar := config.CfgVar{
		EnvName: "INVALID_VAR",
		Field:   "InvalidVar",
	}
	RegisteredVariables = append(RegisteredVariables, invalidVar)

	err := config.LoadConfig(RegisteredVariables, envPrefix, testCfg)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
}

type TestConfig struct {
	PrivateKey *crypto.Ed25519PrivateKey
	Inner      InnerTestConfig
}

type InnerTestConfig struct {
	Val1 string
	Val2 int
}

var (
	RegisteredVariables = []config.CfgVar{
		PrivateKey,
		InnerVal1,
		InnerVal2,
	}

	PrivateKey = config.CfgVar{
		EnvName: "PRIVATE_KEY",
		Field:   "PrivateKey",
		Setter: func(val any) (any, error) {
			if val == nil {
				return nil, nil
			}

			strVal, err := conv.String(val)
			if err != nil {
				return nil, err
			}

			return crypto.Ed25519PrivateKeyFromHex(strVal)
		},
		Required: true,
	}

	InnerVal1 = config.CfgVar{
		EnvName: "INNER_VAL_1",
		Field:   "Inner.Val1",
	}

	InnerVal2 = config.CfgVar{
		EnvName: "INNER_VAL_2",
		Field:   "Inner.Val2",
		Default: 1,
	}
)
