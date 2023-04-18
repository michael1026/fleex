package cmd

import (
	"github.com/michael1026/fleex/pkg/controller"
	"github.com/michael1026/fleex/pkg/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Send a command to a fleet",
	Run: func(cmd *cobra.Command, args []string) {
		var token string

		proxy, _ := rootCmd.PersistentFlags().GetString("proxy")
		utils.SetProxy(proxy)

		fleetName, _ := cmd.Flags().GetString("name")
		command, _ := cmd.Flags().GetString("command")

		providerFlag, _ := cmd.Flags().GetString("provider")
		if providerFlag != "" {
			viper.Set("provider", providerFlag)
		}
		provider := controller.GetProvider(viper.GetString("provider"))
		providerFlag = viper.GetString("provider")

		port, _ := cmd.Flags().GetInt("port")
		username, _ := cmd.Flags().GetString("username")
		password, _ := cmd.Flags().GetString("password")
		if port != -1 {
			viper.Set(providerFlag+".port", port)
		}
		if username != "" {
			viper.Set(providerFlag+".username", username)
		}
		if password != "" {
			viper.Set(providerFlag+".password", password)
		}

		switch provider {
		case controller.PROVIDER_LINODE:
			token = viper.GetString("linode.token")
			port = viper.GetInt("linode.port")
			username = viper.GetString("linode.username")
			password = viper.GetString("linode.password")
		case controller.PROVIDER_DIGITALOCEAN:
			token = viper.GetString("digitalocean.token")
			port = viper.GetInt("digitalocean.port")
			username = viper.GetString("digitalocean.username")
			password = viper.GetString("digitalocean.password")
		case controller.PROVIDER_VULTR:
			token = viper.GetString("vultr.token")
			port = viper.GetInt("vultr.port")
			username = viper.GetString("vultr.username")
			password = viper.GetString("vultr.password")
		}
		controller.RunCommand(fleetName, command, token, port, username, password, provider)

	},
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().StringP("name", "n", "pwn", "Box name")
	runCmd.Flags().StringP("command", "c", "", "Command to send")
	runCmd.Flags().IntP("port", "", -1, "SSH port")
	runCmd.Flags().StringP("username", "U", "", "SSH username")
	runCmd.Flags().StringP("password", "P", "", "SSH password")
	runCmd.Flags().StringP("provider", "p", "", "Service provider (Supported: linode, digitalocean, vultr)")

	runCmd.MarkFlagRequired("command")
}
