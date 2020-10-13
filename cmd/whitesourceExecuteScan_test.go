package cmd

import (
	"fmt"
	"github.com/SAP/jenkins-library/pkg/mock"
	"github.com/SAP/jenkins-library/pkg/versioning"
	ws "github.com/SAP/jenkins-library/pkg/whitesource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type whitesourceCoordinatesMock struct {
	GroupID    string
	ArtifactID string
	Version    string
}

type downloadedFile struct {
	sourceURL string
	filePath  string
}

type npmInstall struct {
	currentDir  string
	packageJSON []string
}

type whitesourceUtilsMock struct {
	*mock.FilesMock
	*mock.ExecMockRunner
	coordinates             whitesourceCoordinatesMock
	usedBuildTool           string
	usedBuildDescriptorFile string
	usedOptions             versioning.Options
	downloadedFiles         []downloadedFile
	npmInstalledModules     []npmInstall
}

func (w *whitesourceUtilsMock) DownloadFile(url, filename string, _ http.Header, _ []*http.Cookie) error {
	w.downloadedFiles = append(w.downloadedFiles, downloadedFile{sourceURL: url, filePath: filename})
	return nil
}

func (w *whitesourceUtilsMock) FileOpen(name string, flag int, perm os.FileMode) (wsFile, error) {
	return w.Open(name, flag, perm)
}

func (w *whitesourceUtilsMock) RemoveAll(path string) error {
	// TODO: Implement in FS Mock
	return nil
}

func (w *whitesourceUtilsMock) GetArtifactCoordinates(buildTool, buildDescriptorFile string,
	options *versioning.Options) (versioning.Coordinates, error) {
	w.usedBuildTool = buildTool
	w.usedBuildDescriptorFile = buildDescriptorFile
	w.usedOptions = *options
	return w.coordinates, nil
}

func (w *whitesourceUtilsMock) FindPackageJSONFiles(_ *ws.NPMScanOptions) ([]string, error) {
	matches, _ := w.Glob("**/package.json")
	return matches, nil
}

func (w *whitesourceUtilsMock) InstallAllNPMDependencies(_ *ws.NPMScanOptions, packageJSONs []string) error {
	w.npmInstalledModules = append(w.npmInstalledModules, npmInstall{
		currentDir:  w.CurrentDir,
		packageJSON: packageJSONs,
	})
	return nil
}

const wsTimeNow = "2010-05-10 00:15:42"

func (w *whitesourceUtilsMock) Now() time.Time {
	now, _ := time.Parse("2006-01-02 15:04:05", wsTimeNow)
	return now
}

func newWhitesourceUtilsMock() *whitesourceUtilsMock {
	return &whitesourceUtilsMock{
		FilesMock:      &mock.FilesMock{},
		ExecMockRunner: &mock.ExecMockRunner{},
		coordinates: whitesourceCoordinatesMock{
			GroupID:    "mock-group-id",
			ArtifactID: "mock-artifact-id",
			Version:    "1.0.42",
		},
	}
}

func TestResolveProjectIdentifiers(t *testing.T) {
	t.Parallel()
	t.Run("happy path", func(t *testing.T) {
		// init
		config := ScanOptions{
			BuildTool:           "mta",
			BuildDescriptorFile: "my-mta.yml",
			VersioningModel:     "major",
			ProductName:         "mock-product",
			M2Path:              "m2/path",
			ProjectSettingsFile: "project-settings.xml",
			GlobalSettingsFile:  "global-settings.xml",
		}
		utilsMock := newWhitesourceUtilsMock()
		systemMock := ws.NewSystemMock("ignored")
		scan := newWhitesourceScan(&config)
		// test
		err := resolveProjectIdentifiers(&config, scan, utilsMock, systemMock)
		// assert
		if assert.NoError(t, err) {
			assert.Equal(t, "mock-group-id-mock-artifact-id", scan.AggregateProjectName)
			assert.Equal(t, "1", config.ProductVersion)
			assert.Equal(t, "mock-product-token", config.ProductToken)
			assert.Equal(t, "mta", utilsMock.usedBuildTool)
			assert.Equal(t, "my-mta.yml", utilsMock.usedBuildDescriptorFile)
			assert.Equal(t, "project-settings.xml", utilsMock.usedOptions.ProjectSettingsFile)
			assert.Equal(t, "global-settings.xml", utilsMock.usedOptions.GlobalSettingsFile)
			assert.Equal(t, "m2/path", utilsMock.usedOptions.M2Path)
		}
	})
	t.Run("retrieves token for configured project name", func(t *testing.T) {
		// init
		config := ScanOptions{
			BuildTool:           "mta",
			BuildDescriptorFile: "my-mta.yml",
			VersioningModel:     "major",
			ProductName:         "mock-product",
			ProjectName:         "mock-project",
		}
		utilsMock := newWhitesourceUtilsMock()
		systemMock := ws.NewSystemMock("ignored")
		scan := newWhitesourceScan(&config)
		// test
		err := resolveProjectIdentifiers(&config, scan, utilsMock, systemMock)
		// assert
		if assert.NoError(t, err) {
			assert.Equal(t, "mock-project", scan.AggregateProjectName)
			assert.Equal(t, "1", config.ProductVersion)
			assert.Equal(t, "mock-product-token", config.ProductToken)
			assert.Equal(t, "mta", utilsMock.usedBuildTool)
			assert.Equal(t, "my-mta.yml", utilsMock.usedBuildDescriptorFile)
			assert.Equal(t, "mock-project-token", config.ProjectToken)
		}
	})
	t.Run("product not found", func(t *testing.T) {
		// init
		config := ScanOptions{
			BuildTool:       "mta",
			VersioningModel: "major",
			ProductName:     "does-not-exist",
		}
		utilsMock := newWhitesourceUtilsMock()
		systemMock := ws.NewSystemMock("ignored")
		scan := newWhitesourceScan(&config)
		// test
		err := resolveProjectIdentifiers(&config, scan, utilsMock, systemMock)
		// assert
		assert.EqualError(t, err, "no product with name 'does-not-exist' found in Whitesource")
	})
}

func TestExecuteScanUA(t *testing.T) {
	t.Parallel()
	t.Run("happy path UA", func(t *testing.T) {
		// init
		config := ScanOptions{
			ScanType:         "unified-agent",
			OrgToken:         "org-token",
			UserToken:        "user-token",
			ProductName:      "mock-product",
			ProjectName:      "mock-project",
			ProductVersion:   "product-version",
			AgentDownloadURL: "https://download.ua.org/agent.jar",
			AgentFileName:    "unified-agent.jar",
			ConfigFilePath:   "ua.cfg",
			M2Path:           ".pipeline/m2",
		}
		utilsMock := newWhitesourceUtilsMock()
		utilsMock.AddFile("wss-generated-file.config", []byte("key=value"))
		scan := newWhitesourceScan(&config)
		// test
		err := executeScan(&config, scan, utilsMock)
		// many assert
		require.NoError(t, err)

		content, err := utilsMock.FileRead("ua.cfg")
		require.NoError(t, err)
		contentAsString := string(content)
		assert.Contains(t, contentAsString, "key=value\n")
		assert.Contains(t, contentAsString, "gradle.aggregateModules=true\n")
		assert.Contains(t, contentAsString, "maven.aggregateModules=true\n")
		assert.Contains(t, contentAsString, "maven.m2RepositoryPath=.pipeline/m2\n")
		assert.Contains(t, contentAsString, "excludes=")

		require.Len(t, utilsMock.Calls, 4)
		fmt.Printf("calls: %v\n", utilsMock.Calls)
		expectedCall := mock.ExecCall{
			Exec: "java",
			Params: []string{
				"-jar",
				config.AgentFileName,
				"-d", ".",
				"-c", config.ConfigFilePath,
				"-apiKey", config.OrgToken,
				"-userKey", config.UserToken,
				"-project", config.ProjectName,
				"-product", config.ProductName,
				"-productVersion", config.ProductVersion,
			},
		}
		assert.Equal(t, expectedCall, utilsMock.Calls[3])
	})
	t.Run("UA is downloaded", func(t *testing.T) {
		// init
		config := ScanOptions{
			ScanType:         "unified-agent",
			AgentDownloadURL: "https://download.ua.org/agent.jar",
			AgentFileName:    "unified-agent.jar",
		}
		utilsMock := newWhitesourceUtilsMock()
		utilsMock.AddFile("wss-generated-file.config", []byte("dummy"))
		scan := newWhitesourceScan(&config)
		// test
		err := executeScan(&config, scan, utilsMock)
		// many assert
		require.NoError(t, err)
		require.Len(t, utilsMock.downloadedFiles, 1)
		assert.Equal(t, "https://download.ua.org/agent.jar", utilsMock.downloadedFiles[0].sourceURL)
		assert.Equal(t, "unified-agent.jar", utilsMock.downloadedFiles[0].filePath)
	})
	t.Run("UA is NOT downloaded", func(t *testing.T) {
		// init
		config := ScanOptions{
			ScanType:         "unified-agent",
			AgentDownloadURL: "https://download.ua.org/agent.jar",
			AgentFileName:    "unified-agent.jar",
		}
		utilsMock := newWhitesourceUtilsMock()
		utilsMock.AddFile("wss-generated-file.config", []byte("dummy"))
		utilsMock.AddFile("unified-agent.jar", []byte("dummy"))
		scan := newWhitesourceScan(&config)
		// test
		err := executeScan(&config, scan, utilsMock)
		// many assert
		require.NoError(t, err)
		assert.Len(t, utilsMock.downloadedFiles, 0)
	})
}

const whiteSourceConfig = "whitesource.config.json"

func TestExecuteScanMTA(t *testing.T) {
	const pomXML = `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 http://maven.apache.org/xsd/maven-4.0.0.xsd">
    <modelVersion>4.0.0</modelVersion>
    <artifactId>my-artifact-id</artifactId>
    <packaging>jar</packaging>
</project>
`
	config := ScanOptions{
		BuildTool:      "mta",
		OrgToken:       "org-token",
		UserToken:      "user-token",
		ProductName:    "mock-product",
		ProjectName:    "mock-project",
		ProductVersion: "product-version",
	}

	t.Parallel()
	t.Run("happy path MTA", func(t *testing.T) {
		// init
		utilsMock := newWhitesourceUtilsMock()
		utilsMock.AddFile("pom.xml", []byte(pomXML))
		utilsMock.AddFile("package.json", []byte(`{"name":"my-module-name"}`))
		scan := newWhitesourceScan(&config)
		// test
		err := executeScan(&config, scan, utilsMock)
		// assert
		require.NoError(t, err)
		expectedCalls := []mock.ExecCall{
			{
				Exec: "mvn",
				Params: []string{
					"--file",
					"pom.xml",
					"-Dorg.whitesource.orgToken=org-token",
					"-Dorg.whitesource.product=mock-product",
					"-Dorg.whitesource.checkPolicies=true",
					"-Dorg.whitesource.failOnError=true",
					"-Dorg.whitesource.aggregateProjectName=mock-project",
					"-Dorg.whitesource.aggregateModules=true",
					"-Dorg.whitesource.userKey=user-token",
					"-Dorg.whitesource.productVersion=product-version",
					"-Dorg.slf4j.simpleLogger.log.org.apache.maven.cli.transfer.Slf4jMavenTransferListener=warn",
					"--batch-mode",
					"org.whitesource:whitesource-maven-plugin:19.5.1:update",
				},
			},
			{
				Exec: "npm",
				Params: []string{
					"ls",
				},
			},
			{
				Exec: "npx",
				Params: []string{
					"whitesource",
					"run",
				},
			},
		}
		assert.Equal(t, expectedCalls, utilsMock.Calls)
		assert.True(t, utilsMock.HasWrittenFile(whiteSourceConfig))
		assert.True(t, utilsMock.HasRemovedFile(whiteSourceConfig))
		assert.Equal(t, expectedCalls, utilsMock.Calls)
	})
	t.Run("MTA with only maven modules", func(t *testing.T) {
		// init
		utilsMock := newWhitesourceUtilsMock()
		utilsMock.AddFile("pom.xml", []byte(pomXML))
		scan := newWhitesourceScan(&config)
		// test
		err := executeScan(&config, scan, utilsMock)
		// assert
		require.NoError(t, err)
		expectedCalls := []mock.ExecCall{
			{
				Exec: "mvn",
				Params: []string{
					"--file",
					"pom.xml",
					"-Dorg.whitesource.orgToken=org-token",
					"-Dorg.whitesource.product=mock-product",
					"-Dorg.whitesource.checkPolicies=true",
					"-Dorg.whitesource.failOnError=true",
					"-Dorg.whitesource.aggregateProjectName=mock-project",
					"-Dorg.whitesource.aggregateModules=true",
					"-Dorg.whitesource.userKey=user-token",
					"-Dorg.whitesource.productVersion=product-version",
					"-Dorg.slf4j.simpleLogger.log.org.apache.maven.cli.transfer.Slf4jMavenTransferListener=warn",
					"--batch-mode",
					"org.whitesource:whitesource-maven-plugin:19.5.1:update",
				},
			},
		}
		assert.Equal(t, expectedCalls, utilsMock.Calls)
		assert.False(t, utilsMock.HasWrittenFile(whiteSourceConfig))
		assert.Equal(t, expectedCalls, utilsMock.Calls)
	})
	t.Run("MTA with only NPM modules", func(t *testing.T) {
		// init
		utilsMock := newWhitesourceUtilsMock()
		utilsMock.AddFile("package.json", []byte(`{"name":"my-module-name"}`))
		scan := newWhitesourceScan(&config)
		// test
		err := executeScan(&config, scan, utilsMock)
		// assert
		require.NoError(t, err)
		expectedCalls := []mock.ExecCall{
			{
				Exec: "npm",
				Params: []string{
					"ls",
				},
			},
			{
				Exec: "npx",
				Params: []string{
					"whitesource",
					"run",
				},
			},
		}
		assert.Equal(t, expectedCalls, utilsMock.Calls)
		assert.True(t, utilsMock.HasWrittenFile(whiteSourceConfig))
		assert.True(t, utilsMock.HasRemovedFile(whiteSourceConfig))
		assert.Equal(t, expectedCalls, utilsMock.Calls)
	})
	t.Run("MTA with neither Maven nor NPM modules results in error", func(t *testing.T) {
		// init
		utilsMock := newWhitesourceUtilsMock()
		scan := newWhitesourceScan(&config)
		// test
		err := executeScan(&config, scan, utilsMock)
		// assert
		assert.EqualError(t, err, "neither Maven nor NPM modules found, no scan performed")
	})
}

func TestBlockUntilProjectIsUpdated(t *testing.T) {
	t.Parallel()
	t.Run("already new enough", func(t *testing.T) {
		// init
		nowString := "2010-05-30 00:15:00 +0100"
		now, err := time.Parse(whitesourceDateTimeLayout, nowString)
		if err != nil {
			t.Fatalf(err.Error())
		}
		lastUpdatedDate := "2010-05-30 00:15:01 +0100"
		systemMock := ws.NewSystemMock(lastUpdatedDate)
		// test
		err = blockUntilProjectIsUpdated(systemMock.Projects[0].Token, systemMock, now, 2*time.Second, 1*time.Second, 2*time.Second)
		// assert
		assert.NoError(t, err)
	})
	t.Run("timeout while polling", func(t *testing.T) {
		// init
		nowString := "2010-05-30 00:15:00 +0100"
		now, err := time.Parse(whitesourceDateTimeLayout, nowString)
		if err != nil {
			t.Fatalf(err.Error())
		}
		lastUpdatedDate := "2010-05-30 00:07:00 +0100"
		systemMock := ws.NewSystemMock(lastUpdatedDate)
		// test
		err = blockUntilProjectIsUpdated(systemMock.Projects[0].Token, systemMock, now, 2*time.Second, 1*time.Second, 1*time.Second)
		// assert
		if assert.Error(t, err) {
			assert.Contains(t, err.Error(), "timeout while waiting")
		}
	})
	t.Run("timeout while polling, no update time", func(t *testing.T) {
		// init
		nowString := "2010-05-30 00:15:00 +0100"
		now, err := time.Parse(whitesourceDateTimeLayout, nowString)
		if err != nil {
			t.Fatalf(err.Error())
		}
		systemMock := ws.NewSystemMock("")
		// test
		err = blockUntilProjectIsUpdated(systemMock.Projects[0].Token, systemMock, now, 2*time.Second, 1*time.Second, 1*time.Second)
		// assert
		if assert.Error(t, err) {
			assert.Contains(t, err.Error(), "timeout while waiting")
		}
	})
}

func TestPersistScannedProjects(t *testing.T) {
	resource := filepath.Join(".pipeline", "commonPipelineEnvironment", "custom", "whitesourceProjectNames")

	t.Parallel()
	t.Run("write 1 scanned projects", func(t *testing.T) {
		// init
		config := &ScanOptions{ProductVersion: "1"}
		utils := newWhitesourceUtilsMock()
		scan := newWhitesourceScan(config)
		_ = scan.AppendScannedProject("project")
		// test
		err := persistScannedProjects(config, scan, utils)
		// assert
		if assert.NoError(t, err) && assert.True(t, utils.HasWrittenFile(resource)) {
			contents, _ := utils.FileRead(resource)
			assert.Equal(t, "project - 1", string(contents))
		}
	})
	t.Run("write 2 scanned projects", func(t *testing.T) {
		// init
		config := &ScanOptions{ProductVersion: "1"}
		utils := newWhitesourceUtilsMock()
		scan := newWhitesourceScan(config)
		_ = scan.AppendScannedProject("project-app")
		_ = scan.AppendScannedProject("project-db")
		// test
		err := persistScannedProjects(config, scan, utils)
		// assert
		if assert.NoError(t, err) && assert.True(t, utils.HasWrittenFile(resource)) {
			contents, _ := utils.FileRead(resource)
			assert.Equal(t, "project-app - 1,project-db - 1", string(contents))
		}
	})
	t.Run("write no projects", func(t *testing.T) {
		// init
		config := &ScanOptions{ProductVersion: "1"}
		utils := newWhitesourceUtilsMock()
		scan := newWhitesourceScan(config)
		// test
		err := persistScannedProjects(config, scan, utils)
		// assert
		if assert.NoError(t, err) && assert.True(t, utils.HasWrittenFile(resource)) {
			contents, _ := utils.FileRead(resource)
			assert.Equal(t, "", string(contents))
		}
	})
	t.Run("write aggregated project", func(t *testing.T) {
		// init
		config := &ScanOptions{ProjectName: "project", ProductVersion: "1"}
		utils := newWhitesourceUtilsMock()
		scan := newWhitesourceScan(config)
		// test
		err := persistScannedProjects(config, scan, utils)
		// assert
		if assert.NoError(t, err) && assert.True(t, utils.HasWrittenFile(resource)) {
			contents, _ := utils.FileRead(resource)
			assert.Equal(t, "project - 1", string(contents))
		}
	})
}

func TestAggregateVersionWideLibraries(t *testing.T) {
	t.Parallel()
	t.Run("happy path", func(t *testing.T) {
		// init
		config := &ScanOptions{
			ProductToken:        "mock-product-token",
			ProductVersion:      "1",
			ReportDirectoryName: "mock-reports",
		}
		utils := newWhitesourceUtilsMock()
		system := ws.NewSystemMock("2010-05-30 00:15:00 +0100")
		// test
		err := aggregateVersionWideLibraries(config, utils, system)
		// assert
		resource := filepath.Join("mock-reports", "libraries-20100510-001542.csv")
		if assert.NoError(t, err) && assert.True(t, utils.HasWrittenFile(resource)) {
			contents, _ := utils.FileRead(resource)
			asString := string(contents)
			assert.Equal(t, "Library Name, Project Name\nmock-library, mock-project\n", asString)
		}
	})
}

func TestAggregateVersionWideVulnerabilities(t *testing.T) {
	t.Parallel()
	t.Run("happy path", func(t *testing.T) {
		// init
		config := &ScanOptions{
			ProductToken:        "mock-product-token",
			ProductVersion:      "1",
			ReportDirectoryName: "mock-reports",
		}
		utils := newWhitesourceUtilsMock()
		system := ws.NewSystemMock("2010-05-30 00:15:00 +0100")
		// test
		err := aggregateVersionWideVulnerabilities(config, utils, system)
		// assert
		resource := filepath.Join("mock-reports", "project-names-aggregated.txt")
		assert.NoError(t, err)
		if assert.True(t, utils.HasWrittenFile(resource)) {
			contents, _ := utils.FileRead(resource)
			asString := string(contents)
			assert.Equal(t, "mock-project - 1\n", asString)
		}
		reportSheet := filepath.Join("mock-reports", "vulnerabilities-20100510-001542.xlsx")
		sheetContents, err := utils.FileRead(reportSheet)
		assert.NoError(t, err)
		assert.NotEmpty(t, sheetContents)
	})
}

func TestCheckAndReportScanResults(t *testing.T) {
	t.Parallel()
	t.Run("no reports requested", func(t *testing.T) {
		// init
		config := &ScanOptions{
			ProductToken:        "mock-product-token",
			ProjectToken:        "mock-project-token",
			ProductVersion:      "1",
			ReportDirectoryName: "mock-reports",
		}
		scan := newWhitesourceScan(config)
		utils := newWhitesourceUtilsMock()
		system := ws.NewSystemMock(time.Now().Format(whitesourceDateTimeLayout))
		// test
		err := checkAndReportScanResults(config, scan, utils, system)
		// assert
		assert.NoError(t, err)
		vPath := filepath.Join("report-dir", "mock-project-vulnerability-report.txt")
		assert.False(t, utils.HasWrittenFile(vPath))
		rPath := filepath.Join("report-dir", "mock-project-risk-report.pdf")
		assert.False(t, utils.HasWrittenFile(rPath))
	})
	t.Run("check vulnerabilities - invalid limit", func(t *testing.T) {
		// init
		config := &ScanOptions{
			SecurityVulnerabilities: true,
			CvssSeverityLimit:       "invalid",
		}
		scan := newWhitesourceScan(config)
		utils := newWhitesourceUtilsMock()
		system := ws.NewSystemMock(time.Now().Format(whitesourceDateTimeLayout))
		// test
		err := checkAndReportScanResults(config, scan, utils, system)
		// assert
		assert.EqualError(t, err, "failed to parse parameter cvssSeverityLimit (invalid) as floating point number: strconv.ParseFloat: parsing \"invalid\": invalid syntax")
	})
	t.Run("check vulnerabilities - limit not hit", func(t *testing.T) {
		// init
		config := &ScanOptions{
			ProductToken:            "mock-product-token",
			ProjectToken:            "mock-project-token",
			ProductVersion:          "1",
			ReportDirectoryName:     "mock-reports",
			SecurityVulnerabilities: true,
			CvssSeverityLimit:       "6.0",
		}
		scan := newWhitesourceScan(config)
		utils := newWhitesourceUtilsMock()
		system := ws.NewSystemMock(time.Now().Format(whitesourceDateTimeLayout))
		// test
		err := checkAndReportScanResults(config, scan, utils, system)
		// assert
		assert.NoError(t, err)
	})
	t.Run("check vulnerabilities - limit exceeded", func(t *testing.T) {
		// init
		config := &ScanOptions{
			ProductToken:            "mock-product-token",
			ProjectName:             "mock-project - 1",
			ProjectToken:            "mock-project-token",
			ProductVersion:          "1",
			ReportDirectoryName:     "mock-reports",
			SecurityVulnerabilities: true,
			CvssSeverityLimit:       "4",
		}
		scan := newWhitesourceScan(config)
		utils := newWhitesourceUtilsMock()
		system := ws.NewSystemMock(time.Now().Format(whitesourceDateTimeLayout))
		// test
		err := checkAndReportScanResults(config, scan, utils, system)
		// assert
		assert.EqualError(t, err, "1 Open Source Software Security vulnerabilities with CVSS score greater or equal to 4.0 detected in project mock-project - 1")
	})
}
