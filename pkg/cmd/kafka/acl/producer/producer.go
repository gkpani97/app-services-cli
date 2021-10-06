package producer

import (
	"context"
	"fmt"

	"github.com/redhat-developer/app-services-cli/internal/config"
	"github.com/redhat-developer/app-services-cli/pkg/cmd/factory"
	"github.com/redhat-developer/app-services-cli/pkg/connection"
	"github.com/redhat-developer/app-services-cli/pkg/iostreams"
	"github.com/redhat-developer/app-services-cli/pkg/kafka/acl"
	"github.com/redhat-developer/app-services-cli/pkg/localize"
	"github.com/redhat-developer/app-services-cli/pkg/logging"
	"github.com/spf13/cobra"

	kafkainstanceclient "github.com/redhat-developer/app-services-sdk-go/kafkainstance/apiv1internal/client"
)

type options struct {
	Config     config.IConfig
	Connection factory.ConnectionFunc
	Logger     logging.Logger
	IO         *iostreams.IOStreams
	localizer  localize.Localizer
	Context    context.Context

	kafkaID       string
	topic         string
	user          string
	svcAccount    string
	transactionID string
}

// NewProducerACLCommand creates ACL rules to allow principal to produce to topics
func NewProducerACLCommand(f *factory.Factory) *cobra.Command {

	opts := &options{
		Config:     f.Config,
		Connection: f.Connection,
		Logger:     f.Logger,
		IO:         f.IOStreams,
		localizer:  f.Localizer,
		Context:    f.Context,
	}

	cmd := &cobra.Command{
		Use:     "producer",
		Short:   f.Localizer.MustLocalize("kafka.acl.producer.cmd.shortDescription"),
		Long:    f.Localizer.MustLocalize("kafka.acl.producer.cmd.longDescription"),
		Example: f.Localizer.MustLocalize("kafka.acl.producer.cmd.example"),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {

			cfg, err := opts.Config.Load()
			if err != nil {
				return err
			}

			if !cfg.HasKafka() {
				return opts.localizer.MustLocalizeError("kafka.acl.common.error.noKafkaSelected")
			}

			opts.kafkaID = cfg.Services.Kafka.ClusterID

			if opts.user == "" && opts.svcAccount == "" {
				return opts.localizer.MustLocalizeError("kafka.acl.common.error.noPricipalsSelected")
			}

			if opts.user != "" && opts.svcAccount != "" {
				return opts.localizer.MustLocalizeError("kafka.acl.common.error.bothPricipalsSelected")
			}

			return runConsumer(opts)
		},
	}

	cmd.Flags().StringVar(&opts.user, "user", "", opts.localizer.MustLocalize("kafka.acl.common.flag.user.description"))
	cmd.Flags().StringVar(&opts.svcAccount, "service-account", "", opts.localizer.MustLocalize("kafka.acl.common.flag.serviceAccount.description"))
	cmd.Flags().StringVar(&opts.topic, "topic-prefix", "", opts.localizer.MustLocalize("kafka.acl.common.flag.topicPrefix.description"))
	cmd.Flags().StringVar(&opts.transactionID, "transactional-id", "", opts.localizer.MustLocalize("kafka.acl.common.flag.transactionalID.description"))

	return cmd

}

// nolint:funlen
func runConsumer(opts *options) (err error) {

	conn, err := opts.Connection(connection.DefaultConfigRequireMasAuth)
	if err != nil {
		return err
	}

	api, kafkaInstance, err := conn.API().KafkaAdmin(opts.kafkaID)
	if err != nil {
		return err
	}

	kafkaName := kafkaInstance.GetName()

	var topicNameArg string = acl.Wildcard
	var patternArg kafkainstanceclient.AclPatternType = kafkainstanceclient.ACLPATTERNTYPE_LITERAL

	if opts.topic != "" {
		topicNameArg = opts.topic
		patternArg = kafkainstanceclient.ACLPATTERNTYPE_PREFIXED
	}

	var userArg string

	if opts.user != "" {
		userArg = buildPrincipal(opts.user)
	}

	if opts.svcAccount != "" {
		userArg = buildPrincipal(opts.svcAccount)
	}

	aclBindTopicDescribe := *kafkainstanceclient.NewAclBinding(kafkainstanceclient.ACLRESOURCETYPE_TOPIC, topicNameArg, patternArg, userArg, kafkainstanceclient.ACLOPERATION_DESCRIBE, kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW)

	req := api.AclsApi.CreateAcl(opts.Context)

	req = req.AclBinding(aclBindTopicDescribe)

	err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
	if err != nil {
		return err
	}

	aclBindTopicWrite := *kafkainstanceclient.NewAclBinding(kafkainstanceclient.ACLRESOURCETYPE_TOPIC, topicNameArg, patternArg, userArg, kafkainstanceclient.ACLOPERATION_WRITE, kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW)

	req = req.AclBinding(aclBindTopicWrite)

	err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
	if err != nil {
		return err
	}

	aclBindTopicCreate := *kafkainstanceclient.NewAclBinding(kafkainstanceclient.ACLRESOURCETYPE_TOPIC, topicNameArg, patternArg, userArg, kafkainstanceclient.ACLOPERATION_CREATE, kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW)

	req = req.AclBinding(aclBindTopicCreate)

	err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
	if err != nil {
		return err
	}

	if opts.transactionID != "" {
		aclBindTransactionIDWrite := *kafkainstanceclient.NewAclBinding(kafkainstanceclient.ACLRESOURCETYPE_TRANSACTIONAL_ID, opts.transactionID, kafkainstanceclient.ACLPATTERNTYPE_LITERAL, userArg, kafkainstanceclient.ACLOPERATION_WRITE, kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW)

		req = req.AclBinding(aclBindTransactionIDWrite)

		err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
		if err != nil {
			return err
		}

		aclBindTransactionIDDescribe := *kafkainstanceclient.NewAclBinding(kafkainstanceclient.ACLRESOURCETYPE_TRANSACTIONAL_ID, opts.transactionID, kafkainstanceclient.ACLPATTERNTYPE_LITERAL, userArg, kafkainstanceclient.ACLOPERATION_DESCRIBE, kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW)

		req = req.AclBinding(aclBindTransactionIDDescribe)

		err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
		if err != nil {
			return err
		}
	}

	opts.Logger.Info(opts.localizer.MustLocalize("kafka.acl.producer.log.info.aclsCreated", localize.NewEntry("InstanceName", kafkaName)))

	return nil
}

func buildPrincipal(user string) string {
	return fmt.Sprintf("User:%s", user)
}
