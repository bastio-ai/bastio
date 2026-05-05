package governance

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// BuildMDMBundle returns a zip archive containing every artifact IT needs to
// push the extension to managed browsers across the three supported MDM
// channels: Chrome Enterprise hosted policy, Microsoft Intune (ADMX/ADML),
// and Jamf (.mobileconfig).
//
// Layout inside the zip:
//
//	chrome/managed-storage.json    Chrome Enterprise policy
//	intune/bastio-governance.admx  Group Policy template
//	intune/bastio-governance.adml  English language file
//	jamf/bastio-governance.mobileconfig  macOS configuration profile
//	README.md                       deployment instructions
func BuildMDMBundle(orgID uuid.UUID, backendURL, installToken, installSecret string) ([]byte, error) {
	managed := map[string]any{
		"backend_url":         backendURL,
		"org_id":              orgID,
		"installation_token":  installToken,
		"installation_secret": installSecret,
		"telemetry_endpoint":  "/v1/governance/events",
		"override_enabled":    true,
		"default_policy": map[string]string{
			"low":    "log",
			"medium": "warn",
			"high":   "block_redirect",
		},
		"custom_keywords":   []string{},
		"domain_overrides":  []string{},
	}
	managedJSON, err := json.MarshalIndent(managed, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal managed json: %w", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	files := []struct {
		path    string
		content []byte
	}{
		{"chrome/managed-storage.json", managedJSON},
		{"intune/bastio-governance.admx", []byte(intuneADMX)},
		{"intune/bastio-governance.adml", []byte(intuneADML)},
		{"jamf/bastio-governance.mobileconfig", []byte(jamfMobileconfig(orgID, backendURL, installToken, installSecret))},
		{"README.md", []byte(bundleReadme(orgID, backendURL))},
	}

	for _, f := range files {
		fw, err := zw.Create(f.path)
		if err != nil {
			return nil, fmt.Errorf("zip create %s: %w", f.path, err)
		}
		if _, err := fw.Write(f.content); err != nil {
			return nil, fmt.Errorf("zip write %s: %w", f.path, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("zip close: %w", err)
	}
	return buf.Bytes(), nil
}

const intuneADMX = `<?xml version="1.0" encoding="utf-8"?>
<policyDefinitions xmlns:xsd="http://www.w3.org/2001/XMLSchema"
                   xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
                   xmlns="http://www.microsoft.com/GroupPolicy/PolicyDefinitions"
                   revision="1.0" schemaVersion="1.0">
  <policyNamespaces>
    <target prefix="bastio" namespace="Bastio.Policies.Governance"/>
    <using prefix="windows" namespace="Microsoft.Policies.Windows"/>
  </policyNamespaces>
  <resources minRequiredRevision="1.0"/>
  <categories>
    <category name="BASTIO" displayName="$(string.BASTIO)"/>
    <category name="GOVERNANCE" displayName="$(string.GOVERNANCE)">
      <parentCategory ref="BASTIO"/>
    </category>
  </categories>
  <policies>
    <policy name="ManagedStorage"
            class="Both"
            displayName="$(string.ManagedStorage)"
            explainText="$(string.ManagedStorage_Help)"
            presentation="$(presentation.ManagedStorage)"
            key="Software\Policies\Google\Chrome\3rdparty\extensions\${EXTENSION_ID}\policy">
      <parentCategory ref="GOVERNANCE"/>
      <supportedOn ref="windows:SUPPORTED_WindowsVista"/>
      <elements>
        <text id="backend_url" valueName="backend_url" required="true"/>
        <text id="org_id" valueName="org_id" required="true"/>
        <text id="installation_token" valueName="installation_token" required="true"/>
        <text id="installation_secret" valueName="installation_secret" required="true"/>
      </elements>
    </policy>
  </policies>
</policyDefinitions>
`

const intuneADML = `<?xml version="1.0" encoding="utf-8"?>
<policyDefinitionResources xmlns:xsd="http://www.w3.org/2001/XMLSchema"
                           xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
                           xmlns="http://www.microsoft.com/GroupPolicy/PolicyDefinitions"
                           revision="1.0" schemaVersion="1.0">
  <displayName>Bastio Governance</displayName>
  <description>ADMX template for the Bastio Governance browser extension</description>
  <resources>
    <stringTable>
      <string id="BASTIO">Bastio</string>
      <string id="GOVERNANCE">Governance Extension</string>
      <string id="ManagedStorage">Managed Storage Configuration</string>
      <string id="ManagedStorage_Help">Configure the Bastio Governance extension. The installation_secret is the HKDF root used for HMAC signing — keep it confidential. Pull these values from the bastio dashboard "Generate MDM bundle" wizard.</string>
    </stringTable>
    <presentationTable>
      <presentation id="ManagedStorage">
        <textBox refId="backend_url"><label>Backend URL</label></textBox>
        <textBox refId="org_id"><label>Organization ID</label></textBox>
        <textBox refId="installation_token"><label>Installation Token</label></textBox>
        <textBox refId="installation_secret"><label>Installation Secret</label></textBox>
      </presentation>
    </presentationTable>
  </resources>
</policyDefinitionResources>
`

func jamfMobileconfig(orgID uuid.UUID, backendURL, installToken, installSecret string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>PayloadType</key><string>Configuration</string>
  <key>PayloadVersion</key><integer>1</integer>
  <key>PayloadIdentifier</key><string>com.bastio.governance.%s</string>
  <key>PayloadUUID</key><string>%s</string>
  <key>PayloadDisplayName</key><string>Bastio Governance</string>
  <key>PayloadDescription</key><string>Managed configuration for the Bastio Governance browser extension. Replaces the extension's local config with org-controlled values.</string>
  <key>PayloadOrganization</key><string>Bastio</string>
  <key>PayloadScope</key><string>System</string>
  <key>PayloadContent</key>
  <array>
    <dict>
      <key>PayloadType</key><string>com.google.Chrome.extensions</string>
      <key>PayloadVersion</key><integer>1</integer>
      <key>PayloadIdentifier</key><string>com.bastio.governance.chrome.%s</string>
      <key>PayloadUUID</key><string>%s</string>
      <key>PayloadDisplayName</key><string>Bastio Governance — Chrome managed storage</string>
      <key>com.google.Chrome.extensions</key>
      <dict>
        <key>%%EXTENSION_ID%%</key>
        <dict>
          <key>backend_url</key><string>%s</string>
          <key>org_id</key><string>%s</string>
          <key>installation_token</key><string>%s</string>
          <key>installation_secret</key><string>%s</string>
          <key>telemetry_endpoint</key><string>/v1/governance/events</string>
          <key>override_enabled</key><true/>
        </dict>
      </dict>
    </dict>
  </array>
</dict>
</plist>
`, orgID, uuid.New(), orgID, uuid.New(), backendURL, orgID, installToken, installSecret)
}

func bundleReadme(orgID uuid.UUID, backendURL string) string {
	return fmt.Sprintf(`# Bastio Governance — MDM Bundle

Generated for org **%s** targeting **%s**.

This zip contains everything you need to push the Bastio Governance browser
extension to managed Chrome and Edge browsers across your fleet. Pick the
file that matches your MDM tooling.

## chrome/managed-storage.json — Chrome Enterprise hosted policy

For Chrome Browser Cloud Management or Chrome Enterprise:

1. In your Google Admin console, go to **Devices → Chrome → Apps & Extensions**.
2. Add the Bastio Governance extension by ID (Chrome Web Store listing).
3. Under "Policy for extensions", paste the contents of ` + "`chrome/managed-storage.json`" + `.
4. Save and let policy propagation run (typically <1 hour).

## intune/bastio-governance.admx + .adml — Microsoft Intune

For Windows-managed Edge or Chrome via Intune Group Policy:

1. Copy ` + "`bastio-governance.admx`" + ` into ` + "`%%SystemRoot%%\\PolicyDefinitions`" + `.
2. Copy ` + "`bastio-governance.adml`" + ` into ` + "`%%SystemRoot%%\\PolicyDefinitions\\en-US`" + `.
3. Open Group Policy Management → Computer → Administrative Templates →
   Bastio → Governance Extension → Managed Storage Configuration.
4. Fill in the four values: backend_url, org_id, installation_token,
   installation_secret. (Get them from the bastio dashboard.)
5. Apply the GPO to your AI-using user OU.

## jamf/bastio-governance.mobileconfig — Jamf Pro

For macOS-managed Chrome or Edge via Jamf:

1. In Jamf Pro, go to **Computers → Configuration Profiles → Upload**.
2. Upload ` + "`bastio-governance.mobileconfig`" + `.
3. Replace the placeholder ` + "`%%EXTENSION_ID%%`" + ` with the actual Chrome
   Web Store extension ID for Bastio Governance.
4. Scope it to your AI-using computer group and deploy.

## Verifying

After MDM propagation, an end-user can verify the extension is configured
by clicking the Bastio icon in the toolbar. The popup should show:
- Status: **Active** (cyan)
- Organization ID: matches the value in this bundle
- Backend: matches your configured URL

If the popup says "Not configured / Awaiting MDM push", the managed-storage
policy hasn't reached the browser yet — wait for propagation or force a sync.

## Rotating the installation_secret

You can rotate the secret from the dashboard's Governance → Installations
panel. After rotation, regenerate this bundle and re-push via your MDM.
The server keeps the previous secret valid for 24 hours to give propagation
time.

## Privacy

This extension only sends metadata to your backend — no prompt content
ever leaves the browser. Rule IDs and severity are recorded; the matched
text is not.
`, orgID, backendURL)
}
