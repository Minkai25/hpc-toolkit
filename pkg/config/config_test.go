/*
Copyright 2022 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"hpc-toolkit/pkg/modulereader"

	"github.com/pkg/errors"
	"github.com/zclconf/go-cty/cty"
	. "gopkg.in/check.v1"
)

var (
	// Shared IO Values
	simpleYamlFilename string
	tmpTestDir         string

	// Expected/Input Values
	expectedYaml = []byte(`
blueprint_name: simple
vars:
  project_id: test-project
  labels:
    ghpc_blueprint: simple
    deployment_name: deployment_name
terraform_backend_defaults:
  type: gcs
  configuration:
    bucket: hpc-toolkit-tf-state
deployment_groups:
- group: group1
  modules:
  - source: ./modules/network/vpc
    kind: terraform
    id: "vpc"
    settings:
      network_name: $"${var.deployment_name}_net
`)
	testModules = []Module{
		{
			Source:           "./modules/network/vpc",
			Kind:             TerraformKind,
			ID:               "vpc",
			WrapSettingsWith: make(map[string][]string),
			Settings: NewDict(map[string]cty.Value{
				"network_name": cty.StringVal("$\"${var.deployment_name}_net\""),
				"project_id":   cty.StringVal("project_name"),
			}),
		},
	}
	expectedSimpleBlueprint Blueprint = Blueprint{
		BlueprintName: "simple",
		Vars: NewDict(map[string]cty.Value{
			"project_id": cty.StringVal("test-project"),
			"labels": cty.ObjectVal(map[string]cty.Value{
				"ghpc_blueprint":  cty.StringVal("simple"),
				"deployment_name": cty.StringVal("deployment_name"),
			})}),
		DeploymentGroups: []DeploymentGroup{{Name: "DeploymentGroup1", Modules: testModules}},
	}
	// For expand.go
	requiredVar = modulereader.VarInfo{
		Name:        "reqVar",
		Type:        "string",
		Description: "A test required variable",
		Default:     nil,
		Required:    true,
	}
)

// Setup GoCheck
type MySuite struct{}

var _ = Suite(&MySuite{})

func Test(t *testing.T) {
	TestingT(t)
}

// setup opens a temp file to store the yaml and saves it's name
func setup() {
	simpleYamlFile, err := ioutil.TempFile("", "*.yaml")
	if err != nil {
		log.Fatal(err)
	}
	_, err = simpleYamlFile.Write(expectedYaml)
	if err != nil {
		log.Fatal(err)
	}
	simpleYamlFilename = simpleYamlFile.Name()
	simpleYamlFile.Close()

	// Create test directory with simple modules
	tmpTestDir, err = ioutil.TempDir("", "ghpc_config_tests_*")
	if err != nil {
		log.Fatalf("failed to create temp dir for config tests: %e", err)
	}
	moduleDir := filepath.Join(tmpTestDir, "module")
	err = os.Mkdir(moduleDir, 0755)
	if err != nil {
		log.Fatalf("failed to create test module dir: %v", err)
	}
	varFile, err := os.Create(filepath.Join(moduleDir, "variables.tf"))
	if err != nil {
		log.Fatalf("failed to create variables.tf in test module dir: %v", err)
	}
	testVariablesTF := `
    variable "test_variable" {
        description = "Test Variable"
        type        = string
    }`
	_, err = varFile.WriteString(testVariablesTF)
	if err != nil {
		log.Fatalf("failed to write variables.tf in test module dir: %v", err)
	}
}

// Delete the temp YAML file
func teardown() {
	err := os.Remove(simpleYamlFilename)
	if err != nil {
		log.Fatalf("config_test teardown: %v", err)
	}
	err = os.RemoveAll(tmpTestDir)
	if err != nil {
		log.Fatalf(
			"failed to tear down tmp directory (%s) for config unit tests: %v",
			tmpTestDir, err)
	}
}

// util function
func cleanErrorRegexp(errRegexp string) string {
	errRegexp = strings.ReplaceAll(errRegexp, "[", "\\[")
	errRegexp = strings.ReplaceAll(errRegexp, "]", "\\]")
	return errRegexp
}

func setTestModuleInfo(mod Module, info modulereader.ModuleInfo) {
	modulereader.SetModuleInfo(mod.Source, mod.Kind.String(), info)
}

func getDeploymentConfigForTest() DeploymentConfig {
	testModule := Module{
		Source:           "testSource",
		Kind:             TerraformKind,
		ID:               "testModule",
		Use:              []ModuleID{},
		WrapSettingsWith: make(map[string][]string),
	}
	testModuleWithLabels := Module{
		Source:           "./role/source",
		ID:               "testModuleWithLabels",
		Kind:             TerraformKind,
		Use:              []ModuleID{},
		WrapSettingsWith: make(map[string][]string),
		Settings: NewDict(map[string]cty.Value{
			"moduleLabel": cty.StringVal("moduleLabelValue"),
		}),
	}
	testLabelVarInfo := modulereader.VarInfo{Name: "labels"}
	testModuleInfo := modulereader.ModuleInfo{
		Inputs: []modulereader.VarInfo{testLabelVarInfo},
	}
	testBlueprint := Blueprint{
		BlueprintName: "simple",
		Validators:    nil,
		Vars: NewDict(map[string]cty.Value{
			"deployment_name": cty.StringVal("deployment_name"),
			"project_id":      cty.StringVal("test-project"),
		}),
		DeploymentGroups: []DeploymentGroup{
			{
				Name:    "group1",
				Modules: []Module{testModule, testModuleWithLabels},
			},
		},
	}

	dc := DeploymentConfig{Config: testBlueprint}
	setTestModuleInfo(testModule, testModuleInfo)
	setTestModuleInfo(testModuleWithLabels, testModuleInfo)

	// the next two steps simulate relevant steps in ghpc expand
	dc.addMetadataToModules()
	dc.addDefaultValidators()

	return dc
}

func getBasicDeploymentConfigWithTestModule() DeploymentConfig {
	testModuleSource := filepath.Join(tmpTestDir, "module")
	testDeploymentGroup := DeploymentGroup{
		Name: "primary",
		Modules: []Module{
			{
				ID:       "TestModule",
				Kind:     TerraformKind,
				Source:   testModuleSource,
				Settings: NewDict(map[string]cty.Value{"test_variable": cty.StringVal("test_value")}),
			},
		},
	}
	return DeploymentConfig{
		Config: Blueprint{
			BlueprintName:    "simple",
			Vars:             NewDict(map[string]cty.Value{"deployment_name": cty.StringVal("deployment_name")}),
			DeploymentGroups: []DeploymentGroup{testDeploymentGroup},
		},
	}
}

// create a simple multigroup deployment with a use keyword that matches
// one module to another in an earlier group
func getMultiGroupDeploymentConfig() DeploymentConfig {
	testModuleSource0 := filepath.Join(tmpTestDir, "module0")
	testModuleSource1 := filepath.Join(tmpTestDir, "module1")
	testModuleSource2 := filepath.Join(tmpTestDir, "module2")

	matchingIntergroupName := "test_inter_0"
	matchingIntragroupName0 := "test_intra_0"
	matchingIntragroupName1 := "test_intra_1"
	matchingIntragroupName2 := "test_intra_2"

	altProjectIDSetting := "host_project_id"

	testModuleInfo0 := modulereader.ModuleInfo{
		Inputs: []modulereader.VarInfo{
			{
				Name: "deployment_name",
				Type: "string",
			},
			{
				Name: altProjectIDSetting,
				Type: "string",
			},
		},
		Outputs: []modulereader.OutputInfo{
			{
				Name: matchingIntergroupName,
			},
			{
				Name: matchingIntragroupName0,
			},
			{
				Name: matchingIntragroupName1,
			},
			{
				Name: matchingIntragroupName2,
			},
		},
	}
	testModuleInfo1 := modulereader.ModuleInfo{
		Inputs: []modulereader.VarInfo{
			{
				Name: matchingIntragroupName0,
			},
			{
				Name: matchingIntragroupName1,
			},
			{
				Name: matchingIntragroupName2,
			},
		},
		Outputs: []modulereader.OutputInfo{},
	}

	testModuleInfo2 := modulereader.ModuleInfo{
		Inputs: []modulereader.VarInfo{
			{
				Name: "deployment_name",
				Type: "string",
			},
			{
				Name: matchingIntergroupName,
			},
		},
		Outputs: []modulereader.OutputInfo{},
	}

	mod0 := Module{
		ID:     "TestModule0",
		Kind:   TerraformKind,
		Source: testModuleSource0,
		Settings: NewDict(map[string]cty.Value{
			altProjectIDSetting: GlobalRef("project_id").AsExpression().AsValue(),
		}),
		Outputs: []modulereader.OutputInfo{
			{Name: matchingIntergroupName},
		},
	}
	setTestModuleInfo(mod0, testModuleInfo0)

	mod1 := Module{
		ID:     "TestModule1",
		Kind:   TerraformKind,
		Source: testModuleSource1,
		Settings: NewDict(map[string]cty.Value{
			matchingIntragroupName1: cty.StringVal("explicit-intra-value"),
			matchingIntragroupName2: ModuleRef(mod0.ID, matchingIntragroupName2).AsExpression().AsValue(),
		}),
		Use: []ModuleID{mod0.ID},
	}
	setTestModuleInfo(mod1, testModuleInfo1)

	grp0 := DeploymentGroup{
		Name:    "primary",
		Modules: []Module{mod0, mod1},
	}

	mod2 := Module{
		ID:     "TestModule2",
		Kind:   TerraformKind,
		Source: testModuleSource2,
		Use:    []ModuleID{mod0.ID},
	}
	setTestModuleInfo(mod2, testModuleInfo2)

	grp1 := DeploymentGroup{
		Name:    "secondary",
		Modules: []Module{mod2},
	}

	dc := DeploymentConfig{
		Config: Blueprint{
			BlueprintName: "simple",
			Vars: NewDict(map[string]cty.Value{
				"deployment_name": cty.StringVal("deployment_name"),
				"project_id":      cty.StringVal("test-project"),
				"unused_key":      cty.StringVal("unused_value"),
			}),
			DeploymentGroups: []DeploymentGroup{grp0, grp1},
		},
	}

	dc.addMetadataToModules()
	dc.addDefaultValidators()
	return dc
}

func getDeploymentConfigWithTestModuleEmptyKind() DeploymentConfig {
	testModuleSource := filepath.Join(tmpTestDir, "module")
	dummy := NewDict(map[string]cty.Value{"test_variable": cty.StringVal("test_value")})
	testDeploymentGroup := DeploymentGroup{
		Name: "primary",
		Modules: []Module{
			{
				ID:       "TestModule1",
				Source:   testModuleSource,
				Settings: dummy,
			},
			{
				ID:       "TestModule2",
				Kind:     UnknownKind,
				Source:   testModuleSource,
				Settings: dummy,
			},
		},
	}
	return DeploymentConfig{
		Config: Blueprint{
			BlueprintName:    "simple",
			Vars:             dummy,
			DeploymentGroups: []DeploymentGroup{testDeploymentGroup},
		},
	}
}

/* Tests */
// config.go
func (s *MySuite) TestExpandConfig(c *C) {
	dc := getBasicDeploymentConfigWithTestModule()
	dc.ExpandConfig()
}

func (s *MySuite) TestCheckModulesAndGroups(c *C) {
	{ // Duplicate module name same group
		g := DeploymentGroup{Name: "ice", Modules: []Module{{ID: "pony"}, {ID: "pony"}}}
		err := checkModulesAndGroups([]DeploymentGroup{g})
		c.Check(err, ErrorMatches, "module IDs must be unique: pony used more than once")
	}
	{ // Duplicate module name different groups
		ice := DeploymentGroup{Name: "ice", Modules: []Module{{ID: "pony"}}}
		fire := DeploymentGroup{Name: "fire", Modules: []Module{{ID: "pony"}}}
		err := checkModulesAndGroups([]DeploymentGroup{ice, fire})
		c.Check(err, ErrorMatches, "module IDs must be unique: pony used more than once")
	}
	{ // Mixing module kinds
		g := DeploymentGroup{Name: "ice", Modules: []Module{
			{ID: "pony", Kind: PackerKind},
			{ID: "zebra", Kind: TerraformKind},
		}}
		err := checkModulesAndGroups([]DeploymentGroup{g})
		c.Check(err, ErrorMatches, "mixing modules of differing kinds in a deployment group is not supported: deployment group ice, got packer and terraform")
	}
}

func (s *MySuite) TestListUnusedModules(c *C) {
	{ // No modules in "use"
		m := Module{ID: "m"}
		c.Check(m.listUnusedModules(), DeepEquals, []ModuleID{})
	}

	{ // Useful
		m := Module{
			ID:  "m",
			Use: []ModuleID{"w"},
			Settings: NewDict(map[string]cty.Value{
				"x": cty.True.Mark(ProductOfModuleUse{"w"})})}
		c.Check(m.listUnusedModules(), DeepEquals, []ModuleID{})
	}

	{ // Unused
		m := Module{
			ID:  "m",
			Use: []ModuleID{"w", "u"},
			Settings: NewDict(map[string]cty.Value{
				"x": cty.True.Mark(ProductOfModuleUse{"w"})})}
		c.Check(m.listUnusedModules(), DeepEquals, []ModuleID{"u"})
	}
}

func (s *MySuite) TestListUnusedDeploymentVariables(c *C) {
	dc := getDeploymentConfigForTest()
	dc.applyGlobalVariables()

	unusedVars := dc.listUnusedDeploymentVariables()
	c.Assert(unusedVars, DeepEquals, []string{"project_id"})

	dc = getMultiGroupDeploymentConfig()
	dc.applyGlobalVariables()

	unusedVars = dc.listUnusedDeploymentVariables()
	c.Assert(unusedVars, DeepEquals, []string{"unused_key"})
}

func (s *MySuite) TestAddKindToModules(c *C) {
	/* Test addKindToModules() works when nothing to do */
	dc := getBasicDeploymentConfigWithTestModule()
	testMod, _ := dc.Config.Module("TestModule")
	expected := testMod.Kind
	dc.Config.addKindToModules()
	testMod, _ = dc.Config.Module("TestModule")
	c.Assert(testMod.Kind, Equals, expected)

	/* Test addKindToModules() works when kind is absent*/
	dc = getDeploymentConfigWithTestModuleEmptyKind()
	expected = TerraformKind
	dc.Config.addKindToModules()
	testMod, _ = dc.Config.Module("TestModule1")
	c.Assert(testMod.Kind, Equals, expected)

	/* Test addKindToModules() works when kind is empty*/
	dc = getDeploymentConfigWithTestModuleEmptyKind()
	expected = TerraformKind
	dc.Config.addKindToModules()
	testMod, _ = dc.Config.Module("TestModule1")
	c.Assert(testMod.Kind, Equals, expected)

	/* Test addKindToModules() does nothing to packer types*/
	moduleID := ModuleID("packerModule")
	expected = PackerKind
	dc = getDeploymentConfigWithTestModuleEmptyKind()
	dc.Config.DeploymentGroups[0].Modules = append(dc.Config.DeploymentGroups[0].Modules, Module{ID: moduleID, Kind: expected})
	dc.Config.addKindToModules()
	testMod, _ = dc.Config.Module(moduleID)
	c.Assert(testMod.Kind, Equals, expected)

	/* Test addKindToModules() does nothing to invalid types*/
	moduleID = "funnyModule"
	expected = ModuleKind{kind: "funnyKind"}
	dc = getDeploymentConfigWithTestModuleEmptyKind()
	dc.Config.DeploymentGroups[0].Modules = append(dc.Config.DeploymentGroups[0].Modules, Module{ID: moduleID, Kind: expected})
	dc.Config.addKindToModules()
	testMod, _ = dc.Config.Module(moduleID)
	c.Assert(testMod.Kind, Equals, expected)
}

func (s *MySuite) TestGetModule(c *C) {
	bp := Blueprint{
		DeploymentGroups: []DeploymentGroup{{
			Modules: []Module{{ID: "blue"}}}},
	}
	{
		m, err := bp.Module("blue")
		c.Check(err, IsNil)
		c.Check(m, Equals, &bp.DeploymentGroups[0].Modules[0])
	}
	{
		m, err := bp.Module("red")
		c.Check(err, NotNil)
		c.Check(m, IsNil)
	}
}

func (s *MySuite) TestDeploymentName(c *C) {
	bp := Blueprint{}
	var e *InputValueError

	// Is deployment_name a valid string?
	bp.Vars.Set("deployment_name", cty.StringVal("yellow"))
	dn, err := bp.DeploymentName()
	c.Assert(dn, Equals, "yellow")
	c.Assert(err, IsNil)

	// Is deployment_name an empty string?
	bp.Vars.Set("deployment_name", cty.StringVal(""))
	dn, err = bp.DeploymentName()
	c.Assert(dn, Equals, "")
	c.Check(errors.As(err, &e), Equals, true)

	// Is deployment_name not a string?
	bp.Vars.Set("deployment_name", cty.NumberIntVal(100))
	dn, err = bp.DeploymentName()
	c.Assert(dn, Equals, "")
	c.Check(errors.As(err, &e), Equals, true)

	// Is deployment_names longer than 63 characters?
	bp.Vars.Set("deployment_name", cty.StringVal("deployment_name-deployment_name-deployment_name-deployment_name-0123"))
	dn, err = bp.DeploymentName()
	c.Assert(dn, Equals, "")
	c.Check(errors.As(err, &e), Equals, true)

	// Does deployment_name contain special characters other than dashes or underscores?
	bp.Vars.Set("deployment_name", cty.StringVal("deployment.name"))
	dn, err = bp.DeploymentName()
	c.Assert(dn, Equals, "")
	c.Check(errors.As(err, &e), Equals, true)

	// Does deployment_name contain capital letters?
	bp.Vars.Set("deployment_name", cty.StringVal("Deployment_name"))
	dn, err = bp.DeploymentName()
	c.Assert(dn, Equals, "")
	c.Check(errors.As(err, &e), Equals, true)

	// Is deployment_name not set?
	bp.Vars = Dict{}
	dn, err = bp.DeploymentName()
	c.Assert(dn, Equals, "")
	c.Check(errors.As(err, &e), Equals, true)
}

func (s *MySuite) TestCheckBlueprintName(c *C) {
	dc := getDeploymentConfigForTest()
	var e *InputValueError

	// Is blueprint_name a valid string?
	err := dc.Config.checkBlueprintName()
	c.Assert(err, IsNil)

	// Is blueprint_name a valid string with an underscore and dash?
	dc.Config.BlueprintName = "blue-print_name"
	err = dc.Config.checkBlueprintName()
	c.Check(err, IsNil)

	// Is blueprint_name an empty string?
	dc.Config.BlueprintName = ""
	err = dc.Config.checkBlueprintName()
	c.Check(errors.As(err, &e), Equals, true)

	// Is blueprint_name longer than 63 characters?
	dc.Config.BlueprintName = "blueprint-name-blueprint-name-blueprint-name-blueprint-name-0123"
	err = dc.Config.checkBlueprintName()
	c.Check(errors.As(err, &e), Equals, true)

	// Does blueprint_name contain special characters other than dashes or underscores?
	dc.Config.BlueprintName = "blueprint.name"
	err = dc.Config.checkBlueprintName()
	c.Check(errors.As(err, &e), Equals, true)

	// Does blueprint_name contain capital letters?
	dc.Config.BlueprintName = "Blueprint_name"
	err = dc.Config.checkBlueprintName()
	c.Check(errors.As(err, &e), Equals, true)
}

func (s *MySuite) TestNewBlueprint(c *C) {
	dc := getDeploymentConfigForTest()
	outFile := filepath.Join(tmpTestDir, "out_TestNewBlueprint.yaml")
	c.Assert(dc.ExportBlueprint(outFile), IsNil)
	newDC, err := NewDeploymentConfig(outFile)
	c.Assert(err, IsNil)
	c.Assert(dc.Config, DeepEquals, newDC.Config)
}

func (s *MySuite) TestImportBlueprint(c *C) {
	obtainedBlueprint, err := importBlueprint(simpleYamlFilename)
	c.Assert(err, IsNil)
	c.Assert(obtainedBlueprint.BlueprintName,
		Equals, expectedSimpleBlueprint.BlueprintName)
	c.Assert(
		obtainedBlueprint.Vars.Get("labels"),
		DeepEquals,
		expectedSimpleBlueprint.Vars.Get("labels"))
	c.Assert(obtainedBlueprint.DeploymentGroups[0].Modules[0].ID,
		Equals, expectedSimpleBlueprint.DeploymentGroups[0].Modules[0].ID)
}

func (s *MySuite) TestImportBlueprint_LabelValidation(c *C) {
	dc := getDeploymentConfigForTest()

	labelName := "my_test_label_name"
	labelValue := "my-valid-label-value"
	invalidLabelName := "my_test_label_name_with_a_bad_char!"
	invalidLabelValue := "some/long/path/with/invalid/characters/and/with/more/than/63/characters!"

	maxLabels := 64

	var err error

	// Simple success case
	dc.Config.Vars.Set("labels", cty.MapVal(map[string]cty.Value{
		labelName: cty.StringVal(labelValue),
	}))
	err = dc.validateVars()
	c.Assert(err, Equals, nil)

	// Succeed on empty value
	dc.Config.Vars.Set("labels", cty.MapVal(map[string]cty.Value{
		labelName: cty.StringVal(""),
	}))
	err = dc.validateVars()
	c.Assert(err, Equals, nil)

	// Succeed on lowercase international character
	dc.Config.Vars.Set("labels", cty.MapVal(map[string]cty.Value{
		"ñ" + labelName: cty.StringVal("ñ"),
	}))
	err = dc.validateVars()
	c.Assert(err, Equals, nil)

	// Succeed on case-less international character
	dc.Config.Vars.Set("labels", cty.MapVal(map[string]cty.Value{
		"ƿ" + labelName: cty.StringVal("ƿ"), // Unicode 01BF, latin character "wynn"
	}))
	err = dc.validateVars()
	c.Assert(err, Equals, nil)

	// Succeed on max number of labels
	largeLabelsMap := map[string]cty.Value{}
	for i := 0; i < maxLabels; i++ {
		largeLabelsMap[labelName+"_"+fmt.Sprint(i)] = cty.StringVal(labelValue)
	}
	dc.Config.Vars.Set("labels", cty.MapVal(largeLabelsMap))
	err = dc.validateVars()
	c.Assert(err, Equals, nil)

	// Invalid label name
	dc.Config.Vars.Set("labels", cty.MapVal(map[string]cty.Value{
		invalidLabelName: cty.StringVal(labelValue),
	}))
	err = dc.validateVars()
	c.Assert(err, ErrorMatches, fmt.Sprintf(`.*name.*'%s: %s'.*`,
		regexp.QuoteMeta(invalidLabelName),
		regexp.QuoteMeta(labelValue)))

	// Invalid label value
	dc.Config.Vars.Set("labels", cty.MapVal(map[string]cty.Value{
		labelName: cty.StringVal(invalidLabelValue),
	}))
	err = dc.validateVars()
	c.Assert(err, ErrorMatches, fmt.Sprintf(`.*value.*'%s: %s'.*`,
		regexp.QuoteMeta(labelName),
		regexp.QuoteMeta(invalidLabelValue)))

	// Too many labels
	tooManyLabelsMap := map[string]cty.Value{}
	for i := 0; i < maxLabels+1; i++ {
		tooManyLabelsMap[labelName+"_"+fmt.Sprint(i)] = cty.StringVal(labelValue)
	}
	dc.Config.Vars.Set("labels", cty.MapVal(tooManyLabelsMap))
	err = dc.validateVars()
	c.Assert(err, ErrorMatches, `vars.labels cannot have more than 64 labels`)

	// Fail on uppercase international character
	dc.Config.Vars.Set("labels", cty.MapVal(map[string]cty.Value{
		labelName: cty.StringVal("Ñ"),
	}))
	err = dc.validateVars()
	c.Assert(err, ErrorMatches, fmt.Sprintf(`.*value.*'%s: %s'.*`,
		regexp.QuoteMeta(labelName),
		regexp.QuoteMeta("Ñ")))

	// Fail on empty name
	dc.Config.Vars.Set("labels", cty.MapVal(map[string]cty.Value{
		"": cty.StringVal(labelValue),
	}))
	err = dc.validateVars()
	c.Assert(err, ErrorMatches, fmt.Sprintf(`.*name.*'%s: %s'.*`,
		"",
		regexp.QuoteMeta(labelValue)))
}

func (s *MySuite) TestImportBlueprint_ExtraField_ThrowsError(c *C) {
	yaml := []byte(`
blueprint_name: hpc-cluster-high-io
# line below is not in our schema
dragon: "Lews Therin Telamon"`)
	file, _ := ioutil.TempFile("", "*.yaml")
	file.Write(yaml)
	filename := file.Name()
	file.Close()

	// should fail on strict unmarshal as field does not match schema
	_, err := importBlueprint(filename)
	c.Check(err, NotNil)
}

func (s *MySuite) TestExportBlueprint(c *C) {
	dc := DeploymentConfig{Config: expectedSimpleBlueprint}
	outFilename := "out_TestExportBlueprint.yaml"
	outFile := filepath.Join(tmpTestDir, outFilename)
	c.Assert(dc.ExportBlueprint(outFile), IsNil)
	fileInfo, err := os.Stat(outFile)
	c.Assert(err, IsNil)
	c.Assert(fileInfo.Name(), Equals, outFilename)
	c.Assert(fileInfo.Size() > 0, Equals, true)
	c.Assert(fileInfo.IsDir(), Equals, false)
}

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	teardown()
	os.Exit(code)
}

func (s *MySuite) TestValidationLevels(c *C) {
	c.Check(isValidValidationLevel(0), Equals, true)
	c.Check(isValidValidationLevel(1), Equals, true)
	c.Check(isValidValidationLevel(2), Equals, true)

	c.Check(isValidValidationLevel(-1), Equals, false)
	c.Check(isValidValidationLevel(3), Equals, false)
}

func (s *MySuite) TestCheckMovedModules(c *C) {
	bp := Blueprint{
		DeploymentGroups: []DeploymentGroup{
			{Modules: []Module{
				{Source: "some/module/that/has/not/moved"}}}}}

	// base case should not err
	c.Assert(bp.checkMovedModules(), IsNil)

	// embedded moved
	bp.DeploymentGroups[0].Modules[0].Source = "community/modules/scheduler/cloud-batch-job"
	c.Assert(bp.checkMovedModules(), NotNil)

	// local moved
	bp.DeploymentGroups[0].Modules[0].Source = "./community/modules/scheduler/cloud-batch-job"
	c.Assert(bp.checkMovedModules(), NotNil)
}

func (s *MySuite) TestValidatorConfigCheck(c *C) {
	const vn = testProjectExistsName // some valid name

	{ // FAIL: names mismatch
		v := validatorConfig{Validator: "who_is_this"}
		err := v.check(vn, []string{})
		c.Check(err, ErrorMatches, "passed wrong validator to test_project_exists implementation")
	}

	{ // OK: names match
		v := validatorConfig{Validator: vn.String()}
		c.Check(v.check(vn, []string{}), IsNil)
	}

	{ // OK: Inputs is equal to required inputs without regard to ordering
		v := validatorConfig{
			Validator: vn.String(),
			Inputs: NewDict(map[string]cty.Value{
				"in0": cty.NilVal,
				"in1": cty.NilVal})}
		c.Check(v.check(vn, []string{"in0", "in1"}), IsNil)
		c.Check(v.check(vn, []string{"in1", "in0"}), IsNil)
	}

	{ // FAIL: inputs are a proper subset of required inputs
		v := validatorConfig{
			Validator: vn.String(),
			Inputs: NewDict(map[string]cty.Value{
				"in0": cty.NilVal,
				"in1": cty.NilVal})}
		err := v.check(vn, []string{"in0", "in1", "in2"})
		c.Check(err, ErrorMatches, missingRequiredInputRegex)
	}

	{ // FAIL: inputs intersect with required inputs but are not a proper subset
		v := validatorConfig{
			Validator: vn.String(),
			Inputs: NewDict(map[string]cty.Value{
				"in0": cty.NilVal,
				"in1": cty.NilVal,
				"in3": cty.NilVal})}
		err := v.check(vn, []string{"in0", "in1", "in2"})
		c.Check(err, ErrorMatches, missingRequiredInputRegex)
	}

	{ // FAIL inputs are a proper superset of required inputs
		v := validatorConfig{
			Validator: vn.String(),
			Inputs: NewDict(map[string]cty.Value{
				"in0": cty.NilVal,
				"in1": cty.NilVal,
				"in2": cty.NilVal,
				"in3": cty.NilVal})}
		err := v.check(vn, []string{"in0", "in1", "in2"})
		c.Check(err, ErrorMatches, "only 3 inputs \\[in0 in1 in2\\] should be provided to test_project_exists")
	}
}

func (s *MySuite) TestCheckBackends(c *C) {
	// Helper to create blueprint with backend blocks only (first one is defaults)
	// and run checkBackends.
	check := func(d TerraformBackend, gb ...TerraformBackend) error {
		gs := []DeploymentGroup{}
		for _, b := range gb {
			gs = append(gs, DeploymentGroup{TerraformBackend: b})
		}
		bp := Blueprint{
			TerraformBackendDefaults: d,
			DeploymentGroups:         gs,
		}
		return checkBackends(bp)
	}
	dummy := TerraformBackend{}

	{ // OK. Absent
		c.Check(checkBackends(Blueprint{}), IsNil)
	}

	{ // OK. Dummies
		c.Check(check(dummy, dummy, dummy), IsNil)
	}

	{ // OK. No variables used
		b := TerraformBackend{Type: "gcs"}
		b.Configuration.
			Set("bucket", cty.StringVal("trenta")).
			Set("impersonate_service_account", cty.StringVal("who"))
		c.Check(check(b), IsNil)
	}

	{ // FAIL. Variable in defaults type
		b := TerraformBackend{Type: "$(vartype)"}
		c.Check(check(b), ErrorMatches, ".*type.*vartype.*")
	}

	{ // FAIL. Variable in group backend type
		b := TerraformBackend{Type: "$(vartype)"}
		c.Check(check(dummy, b), ErrorMatches, ".*type.*vartype.*")
	}

	{ // FAIL. Deployment variable in defaults type
		b := TerraformBackend{Type: "$(vars.type)"}
		c.Check(check(b), ErrorMatches, ".*type.*vars\\.type.*")
	}

	{ // FAIL. HCL literal
		b := TerraformBackend{Type: "((var.zen))"}
		c.Check(check(b), ErrorMatches, ".*type.*zen.*")
	}

	{ // OK. Not a variable
		b := TerraformBackend{Type: "\\$(vartype)"}
		c.Check(check(b), IsNil)
	}

	{ // FAIL. Mid-string variable in defaults type
		b := TerraformBackend{Type: "hugs_$(vartype)_hugs"}
		c.Check(check(b), ErrorMatches, ".*type.*vartype.*")
	}

	{ // FAIL. Variable in defaults configuration
		b := TerraformBackend{Type: "gcs"}
		b.Configuration.Set("bucket", GlobalRef("trenta").AsExpression().AsValue())
		c.Check(check(b), ErrorMatches, ".*can not use variables.*")
	}

	{ // OK. handles nested configuration
		b := TerraformBackend{Type: "gcs"}
		b.Configuration.
			Set("bucket", cty.StringVal("trenta")).
			Set("complex", cty.ObjectVal(map[string]cty.Value{
				"alpha": cty.StringVal("a"),
				"beta":  GlobalRef("boba").AsExpression().AsValue(),
			}))
		c.Check(check(b), ErrorMatches, ".*can not use variables.*")
	}
}

func (s *MySuite) TestSkipValidator(c *C) {
	{
		dc := DeploymentConfig{Config: Blueprint{Validators: nil}}
		c.Check(dc.SkipValidator("zebra"), IsNil)
		c.Check(dc.Config.Validators, DeepEquals, []validatorConfig{
			{Validator: "zebra", Skip: true}})
	}
	{
		dc := DeploymentConfig{Config: Blueprint{Validators: []validatorConfig{
			{Validator: "pony"}}}}
		c.Check(dc.SkipValidator("zebra"), IsNil)
		c.Check(dc.Config.Validators, DeepEquals, []validatorConfig{
			{Validator: "pony"},
			{Validator: "zebra", Skip: true}})
	}
	{
		dc := DeploymentConfig{Config: Blueprint{Validators: []validatorConfig{
			{Validator: "pony"},
			{Validator: "zebra"}}}}
		c.Check(dc.SkipValidator("zebra"), IsNil)
		c.Check(dc.Config.Validators, DeepEquals, []validatorConfig{
			{Validator: "pony"},
			{Validator: "zebra", Skip: true}})
	}
	{
		dc := DeploymentConfig{Config: Blueprint{Validators: []validatorConfig{
			{Validator: "pony"},
			{Validator: "zebra", Skip: true}}}}
		c.Check(dc.SkipValidator("zebra"), IsNil)
		c.Check(dc.Config.Validators, DeepEquals, []validatorConfig{
			{Validator: "pony"},
			{Validator: "zebra", Skip: true}})
	}
	{
		dc := DeploymentConfig{Config: Blueprint{Validators: []validatorConfig{
			{Validator: "zebra"},
			{Validator: "pony"},
			{Validator: "zebra"}}}}
		c.Check(dc.SkipValidator("zebra"), IsNil)
		c.Check(dc.Config.Validators, DeepEquals, []validatorConfig{
			{Validator: "zebra", Skip: true},
			{Validator: "pony"},
			{Validator: "zebra", Skip: true}})
	}

}

func (s *MySuite) TestModuleGroup(c *C) {
	dc := getDeploymentConfigForTest()

	group := dc.Config.DeploymentGroups[0]
	modID := dc.Config.DeploymentGroups[0].Modules[0].ID

	foundGroup := dc.Config.ModuleGroupOrDie(modID)
	c.Assert(foundGroup, DeepEquals, group)

	_, err := dc.Config.ModuleGroup("bad_module_id")
	c.Assert(err, NotNil)
}

func (s *MySuite) TestValidateModuleSettingReference(c *C) {
	mod11 := Module{ID: "mod11", Source: "./mod11", Kind: TerraformKind}
	mod21 := Module{ID: "mod21", Source: "./mod21", Kind: TerraformKind}
	mod22 := Module{ID: "mod22", Source: "./mod22", Kind: TerraformKind}
	pkr := Module{ID: "pkr", Source: "./pkr", Kind: PackerKind}

	bp := Blueprint{
		Vars: NewDict(map[string]cty.Value{
			"var1": cty.True,
		}),
		DeploymentGroups: []DeploymentGroup{
			{Name: "group1", Modules: []Module{mod11}},
			{Name: "groupP", Modules: []Module{pkr}},
			{Name: "group2", Modules: []Module{mod21, mod22}},
		},
	}

	setTestModuleInfo(mod11, modulereader.ModuleInfo{Outputs: []modulereader.OutputInfo{{Name: "out11"}}})
	setTestModuleInfo(mod21, modulereader.ModuleInfo{Outputs: []modulereader.OutputInfo{{Name: "out21"}}})
	setTestModuleInfo(mod22, modulereader.ModuleInfo{Outputs: []modulereader.OutputInfo{{Name: "out22"}}})
	setTestModuleInfo(pkr, modulereader.ModuleInfo{Outputs: []modulereader.OutputInfo{{Name: "outPkr"}}})

	vld := validateModuleSettingReference
	// OK. deployment var
	c.Check(vld(bp, mod11, GlobalRef("var1")), IsNil)

	// FAIL. deployment var doesn't exist
	c.Check(vld(bp, mod11, GlobalRef("var2")), NotNil)

	// FAIL. wrong module
	c.Check(vld(bp, mod11, ModuleRef("jack", "kale")), NotNil)

	// OK. intragroup
	c.Check(vld(bp, mod22, ModuleRef("mod21", "out21")), IsNil)

	// OK. intragroup. out of module order
	c.Check(vld(bp, mod21, ModuleRef("mod22", "out22")), IsNil)

	// OK. intergroup
	c.Check(vld(bp, mod22, ModuleRef("mod11", "out11")), IsNil)

	// FAIL. out of group order
	c.Check(vld(bp, mod11, ModuleRef("mod21", "out21")), NotNil)

	// FAIL. missing output
	c.Check(vld(bp, mod22, ModuleRef("mod21", "kale")), NotNil)

	// Fail. packer module
	c.Check(vld(bp, mod21, ModuleRef("pkr", "outPkr")), NotNil)
}

func (s *MySuite) TestCheckModuleSettings(c *C) {
	m := Module{ID: "m"}
	m.Settings.Set("white", GlobalRef("zebra").AsExpression().AsValue())
	bp := Blueprint{
		DeploymentGroups: []DeploymentGroup{
			{Name: "g", Modules: []Module{m}},
		}}

	c.Check(checkModuleSettings(bp), NotNil)

	bp.Vars.Set("zebra", cty.StringVal("stripes"))
	c.Check(checkModuleSettings(bp), IsNil)
}
