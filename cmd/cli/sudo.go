package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thand-io/agent/internal/models"
)

var sudoCmd = &cobra.Command{
	Use:     "sudo [command...]",
	Short:   "Request local sudo access or run a privileged command",
	Long:    `Request time-bound local sudo access or broker a single privileged command through the local provider.`,
	PreRunE: preRunClientConfigWithSessionE,
	RunE: func(cmd *cobra.Command, args []string) error {
		reason, _ := cmd.Flags().GetString("reason")
		duration, _ := cmd.Flags().GetString("duration")
		target, _ := cmd.Flags().GetString("target")
		request, err := buildLocalSudoElevationRequest(args, reason, duration, target)
		if err != nil {
			return err
		}

		return MakeElevationRequest(request)
	},
}

func buildLocalSudoElevationRequest(args []string, reason, duration, target string) (*models.ElevateRequest, error) {
	if len(reason) == 0 {
		return nil, fmt.Errorf("--reason is required")
	}
	if cfg == nil {
		return nil, fmt.Errorf("configuration is not loaded")
	}

	metadata := models.LocalSudoRequestMetadata{
		Mode:                models.LocalSudoModeTimed,
		TargetAgent:         strings.TrimSpace(target),
		TargetAgentExplicit: len(strings.TrimSpace(target)) > 0,
	}

	if len(args) > 0 {
		metadata.Mode = models.LocalSudoModeCommand
		metadata.Command = append([]string(nil), args...)
	}

	role, err := cfg.GetRoleByName(models.LocalSudoRoleIdentifier)
	if err != nil {
		return nil, fmt.Errorf("local sudo role %q is not configured: %w", models.LocalSudoRoleIdentifier, err)
	}

	request := &models.ElevateRequest{
		Role:     models.CloneRole(role),
		Reason:   reason,
		Duration: duration,
		Metadata: metadata.AsMap(),
	}
	if err := models.NormalizeLocalSudoRequest(
		request,
		cfg.GetProviders().Definitions,
		cfg.GetEnvironmentConfig(),
		models.LocalSudoNormalizeOptions{RequireTarget: false},
	); err != nil {
		return nil, err
	}

	return request, nil
}

func init() {
	requestCmd.AddCommand(sudoCmd)

	sudoCmd.Flags().StringP("duration", "d", "", "Duration of timed sudo access (for example 30m or 1h)")
	sudoCmd.Flags().StringP("reason", "e", "", "Reason for the sudo request")
	sudoCmd.Flags().String("target", "", "Target agent/task queue for local sudo execution")
}
