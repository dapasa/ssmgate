package awsx

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/dapasa/ssmgate/internal/config"
	"github.com/dapasa/ssmgate/internal/prereq"
	"github.com/dapasa/ssmgate/internal/ui"
)

// startShell opens an interactive SSM session on target using the
// account-default document and hands the terminal to the
// session-manager-plugin in the foreground. Blocks until the user exits.
func startShell(ctx context.Context, cfg aws.Config, target, roleHint string) error {
	out, err := ssm.NewFromConfig(cfg).StartSession(ctx, &ssm.StartSessionInput{
		Target: aws.String(target),
	})
	if err != nil {
		return ui.Errorf("StartSession failed for %s: %v\n"+
			"     (check that %s can start sessions on this instance and that its SSM agent is online)", target, err, roleHint)
	}

	cmd, err := pluginCommand(ctx, cfg, out, target, nil)
	if err != nil {
		return err
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// startPortForward starts an SSM port-forwarding session from localPort to
// remoteHost:remotePort through target, running the plugin in the BACKGROUND.
func startPortForward(ctx context.Context, cfg aws.Config, target, remoteHost string, remotePort, localPort int) (*exec.Cmd, error) {
	params := map[string][]string{
		"host":            {remoteHost},
		"portNumber":      {strconv.Itoa(remotePort)},
		"localPortNumber": {strconv.Itoa(localPort)},
	}
	out, err := ssm.NewFromConfig(cfg).StartSession(ctx, &ssm.StartSessionInput{
		Target:       aws.String(target),
		DocumentName: aws.String(config.SSMPortForwardDocument),
		Parameters:   params,
	})
	if err != nil {
		return nil, ui.Errorf("StartSession (port-forward) failed for %s: %v\n"+
			"     (check the role's ssm:StartSession permission and the SSM agent on the tunnel host)", target, err)
	}

	cmd, err := pluginCommand(ctx, cfg, out, target, params)
	if err != nil {
		return nil, err
	}
	// Detach the plugin's stdio; we only care about the local TCP listener.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return nil, ui.Errorf("failed to start %s: %v", prereq.PluginName, err)
	}
	return cmd, nil
}

// pluginArgs builds the positional arguments of the session-manager-plugin,
// using the exact contract the AWS CLI uses:
//
//	session-manager-plugin <StartSession-response-json> <region> StartSession \
//	    <profile> <parameters-json> <ssm-endpoint>
func pluginArgs(region, target string, out *ssm.StartSessionOutput, params map[string][]string) ([]string, error) {
	responseJSON, err := json.Marshal(map[string]any{
		"SessionId":  aws.ToString(out.SessionId),
		"TokenValue": aws.ToString(out.TokenValue),
		"StreamUrl":  aws.ToString(out.StreamUrl),
	})
	if err != nil {
		return nil, fmt.Errorf("could not encode session response: %w", err)
	}
	// Mirrors what the CLI sends so the plugin can rebuild the request context.
	startArgs := map[string]any{"Target": target}
	if params != nil {
		startArgs["DocumentName"] = config.SSMPortForwardDocument
		startArgs["Parameters"] = params
	}
	paramsJSON, err := json.Marshal(startArgs)
	if err != nil {
		return nil, fmt.Errorf("could not encode session parameters: %w", err)
	}
	endpoint := fmt.Sprintf("https://ssm.%s.amazonaws.com", region)
	return []string{string(responseJSON), region, "StartSession", "", string(paramsJSON), endpoint}, nil
}

// pluginCommand assembles the session-manager-plugin invocation. The plugin
// authenticates the websocket with the TokenValue in the response; the
// credentials are still exported to cover KMS-encrypted sessions.
func pluginCommand(ctx context.Context, cfg aws.Config, out *ssm.StartSessionOutput, target string, params map[string][]string) (*exec.Cmd, error) {
	pluginPath, err := prereq.LookPath(prereq.PluginName)
	if err != nil {
		return nil, ui.Errorf("%s not found on PATH.\n     Install: %s", prereq.PluginName, prereq.PluginInstallURL)
	}
	args, err := pluginArgs(cfg.Region, target, out, params)
	if err != nil {
		return nil, ui.Errorf("%v", err)
	}
	cmd := exec.CommandContext(ctx, pluginPath, args...)
	cmd.Env = os.Environ()
	if c, err := cfg.Credentials.Retrieve(ctx); err == nil {
		cmd.Env = append(cmd.Env,
			"AWS_ACCESS_KEY_ID="+c.AccessKeyID,
			"AWS_SECRET_ACCESS_KEY="+c.SecretAccessKey,
			"AWS_SESSION_TOKEN="+c.SessionToken,
			"AWS_REGION="+cfg.Region,
			"AWS_DEFAULT_REGION="+cfg.Region,
		)
	}
	return cmd, nil
}
