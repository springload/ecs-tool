package cmd

import (
	"os"

	"github.com/Shopify/ejson"
	"github.com/apex/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/springload/ecs-tool/lib"
)

var ejsonCmd = &cobra.Command{
	Use:   "ejson -- [processor command with arguments]",
	Short: "decrypt ejson and push to SSM",
	Long: `Decrypt the supplied ejson file and write to the
specified parameter in SSM Parameter Store

The processor commands should read the decrypted ejson file from stdin and should output
the processed content to stdout.
`,
	Run: func(cmd *cobra.Command, args []string) {
		var kmsKey, parameterName, encryptedFile, privateKey, privateKeyDir string
		var processor, pickJsonKeys []string

		if len(args) > 0 {
			processor = args
		} else {
			processor = viper.GetStringSlice("ejson.processor")
		}

		pickJsonKeys = viper.GetStringSlice("ejson.pick_keys")

		parameterName = viper.GetString("ejson.name")
		if parameterName == "" {
			log.Fatal("please set the SSM parameter name to write to with --name or -n")
		}
		kmsKey = viper.GetString("ejson.kms_key")
		if kmsKey == "" {
			log.Fatal("please set the KMS key with --kms-key or -k")
		}

		encryptedFile = viper.GetString("ejson.file")
		if encryptedFile == "" {
			log.Fatal("please specify the ejson file to decrypt")
		}
		// get the private key
		// check the keyvar env variable first
		privateKey = os.Getenv(viper.GetString("ejson.keyvar"))
		// if it's not set there check the keydir
		if privateKey == "" {
			privateKeyDir = os.Getenv(viper.GetString("ejson.dirvar"))
			if privateKeyDir == "" {
				log.Fatal("please supply the private key file either with --keyvar or --dirvar and set the env variables")
			}
		}

		decryptedValue, err := ejson.DecryptFile(encryptedFile, privateKeyDir, privateKey)
		if err != nil {
			log.WithError(err).Fatalf("can't decrypt the file %s", encryptedFile)
		}
		if err := lib.WriteSSMParameter(viper.GetString("profile"), parameterName, kmsKey, string(decryptedValue), processor, pickJsonKeys); err != nil {
			log.WithError(err).Fatal("can't write the ssm parameter")
		}
	},
}

func init() {
	rootCmd.AddCommand(ejsonCmd)
	ejsonCmd.PersistentFlags().StringP("dirvar", "", "EJSON_KEYDIR", "name of the env variable that has the ejson private keys folder")
	ejsonCmd.PersistentFlags().StringP("file", "f", "", "ejson file to decrypt")
	ejsonCmd.PersistentFlags().StringP("name", "n", "", "name of the parameter to write to")
	ejsonCmd.PersistentFlags().StringP("keyvar", "", "EJSON_PRIVATE", "name of the env variable with the private key")
	ejsonCmd.PersistentFlags().StringP("kms-key", "k", "", "kms key Id / alias to use for SSM Parameter encryption")

	viper.BindPFlag("ejson.dirvar", ejsonCmd.PersistentFlags().Lookup("dirvar"))
	viper.BindPFlag("ejson.file", ejsonCmd.PersistentFlags().Lookup("file"))
	viper.BindPFlag("ejson.keyvar", ejsonCmd.PersistentFlags().Lookup("keyvar"))
	viper.BindPFlag("ejson.kms_key", ejsonCmd.PersistentFlags().Lookup("kms-key"))
	viper.BindPFlag("ejson.name", ejsonCmd.PersistentFlags().Lookup("name"))
}
