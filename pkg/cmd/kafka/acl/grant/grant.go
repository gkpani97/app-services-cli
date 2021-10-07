package grant

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

	kafkaID     string
	topic       string
	user        string
	svcAccount  string
	group       string
	producer    bool
	consumer    bool
	topicPrefix string
	groupPrefix string
	admin       bool
}

// NewGrantPermissionsACLCommand creates a series of ACL rules
func NewGrantPermissionsACLCommand(f *factory.Factory) *cobra.Command {

	opts := &options{
		Config:     f.Config,
		Connection: f.Connection,
		Logger:     f.Logger,
		IO:         f.IOStreams,
		localizer:  f.Localizer,
		Context:    f.Context,
	}

	cmd := &cobra.Command{
		Use:     "grant-permissions",
		Short:   f.Localizer.MustLocalize("kafka.acl.grantPermissions.cmd.shortDescription"),
		Long:    f.Localizer.MustLocalize("kafka.acl.grantPermissions.cmd.longDescription"),
		Example: f.Localizer.MustLocalize("kafka.acl.grantPermissions.cmd.example"),
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

			if opts.admin && opts.user == "" {
				return opts.localizer.MustLocalizeError("kafka.acl.grantPermissions.error.user.required")
			}

			if opts.admin && (opts.consumer || opts.producer) {
				return opts.localizer.MustLocalizeError("kafka.acl.grantPermissions.admin.error.notAllowed")
			}

			if opts.admin && (opts.topicPrefix != "" || opts.groupPrefix != "" || opts.topic != "" || opts.group != "") {
				return opts.localizer.MustLocalizeError("kafka.acl.grantPermissions.admin.flags.error.notAllowed")
			}

			if !opts.consumer && (opts.group != "" || opts.groupPrefix != "") {
				return opts.localizer.MustLocalizeError("kafka.acl.grantPermissions.group.error.notAllowed")
			}

			if (opts.topic == "" && opts.topicPrefix == "") && (opts.consumer || opts.producer) {
				return opts.localizer.MustLocalizeError("kafka.acl.grantPermissions.topic.error.required")
			}

			if opts.consumer && opts.group == "" && opts.groupPrefix == "" {
				return opts.localizer.MustLocalizeError("kafka.acl.grantPermissions.group.error.required")
			}

			if opts.topicPrefix != "" && opts.topic != "" {
				return opts.localizer.MustLocalizeError("kafka.acl.grantPermissions.prefix.error.notAllowed")
			}

			if opts.groupPrefix != "" && opts.group != "" {
				return opts.localizer.MustLocalizeError("kafka.acl.grantPermissions.prefix.error.notAllowed")
			}

			return runGrantPermissions(opts)
		},
	}

	cmd.Flags().StringVar(&opts.user, "user", "", opts.localizer.MustLocalize("kafka.acl.common.flag.user.description"))
	cmd.Flags().StringVar(&opts.svcAccount, "service-account", "", opts.localizer.MustLocalize("kafka.acl.common.flag.serviceAccount.description"))
	cmd.Flags().StringVar(&opts.topic, "topic", "", opts.localizer.MustLocalize("kafka.acl.common.flag.topic.description"))
	cmd.Flags().StringVar(&opts.group, "group", "", opts.localizer.MustLocalize("kafka.acl.common.flag.group.description"))
	cmd.Flags().BoolVar(&opts.consumer, "consumer", false, opts.localizer.MustLocalize("kafka.acl.grantPermissions.flag.consumer.description"))
	cmd.Flags().BoolVar(&opts.producer, "producer", false, opts.localizer.MustLocalize("kafka.acl.grantPermissions.flag.producer.description"))
	cmd.Flags().StringVar(&opts.topicPrefix, "topic-prefix", "", opts.localizer.MustLocalize("kafka.acl.common.flag.topicPrefix.description"))
	cmd.Flags().StringVar(&opts.groupPrefix, "group-prefix", "", opts.localizer.MustLocalize("kafka.acl.common.flag.groupPrefix.description"))
	cmd.Flags().BoolVar(&opts.admin, "admin", false, opts.localizer.MustLocalize("kafka.acl.grantPermissions.flag.admin.description"))

	return cmd
}

// nolint:funlen
func runGrantPermissions(opts *options) (err error) {

	conn, err := opts.Connection(connection.DefaultConfigRequireMasAuth)
	if err != nil {
		return err
	}

	api, kafkaInstance, err := conn.API().KafkaAdmin(opts.kafkaID)
	if err != nil {
		return err
	}

	kafkaName := kafkaInstance.GetName()

	var topicNameArg string
	var groupIdArg string
	var topicPatternArg = kafkainstanceclient.ACLPATTERNTYPE_LITERAL
	var groupPatternArg = kafkainstanceclient.ACLPATTERNTYPE_LITERAL

	var userArg string

	if opts.topic != "" {
		topicNameArg = opts.topic
	}

	if opts.topicPrefix != "" {
		topicNameArg = opts.topicPrefix
		topicPatternArg = kafkainstanceclient.ACLPATTERNTYPE_PREFIXED
	}

	if opts.group != "" {
		groupIdArg = opts.group
	}

	if opts.groupPrefix != "" {
		groupIdArg = opts.group
		groupPatternArg = kafkainstanceclient.ACLPATTERNTYPE_PREFIXED
	}

	if opts.user != "" {
		userArg = buildPrincipal(opts.user)
	}

	if opts.svcAccount != "" {
		userArg = buildPrincipal(opts.svcAccount)
	}

	req := api.AclsApi.CreateAcl(opts.Context)

	if opts.producer || opts.consumer {
		aclBindTopicDescribe := *kafkainstanceclient.NewAclBinding(
			kafkainstanceclient.ACLRESOURCETYPE_TOPIC,
			topicNameArg,
			topicPatternArg,
			userArg,
			kafkainstanceclient.ACLOPERATION_DESCRIBE,
			kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW,
		)

		req = req.AclBinding(aclBindTopicDescribe)

		err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
		if err != nil {
			return err
		}
	}

	if opts.consumer {

		aclBindTopicRead := *kafkainstanceclient.NewAclBinding(
			kafkainstanceclient.ACLRESOURCETYPE_TOPIC,
			topicNameArg,
			topicPatternArg,
			userArg,
			kafkainstanceclient.ACLOPERATION_READ,
			kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW,
		)

		req = req.AclBinding(aclBindTopicRead)

		err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
		if err != nil {
			return err
		}

		aclBindGroupRead := *kafkainstanceclient.NewAclBinding(
			kafkainstanceclient.ACLRESOURCETYPE_GROUP,
			groupIdArg,
			groupPatternArg,
			userArg,
			kafkainstanceclient.ACLOPERATION_READ,
			kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW,
		)

		req = api.AclsApi.CreateAcl(opts.Context).AclBinding(aclBindGroupRead)

		err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
		if err != nil {
			return err
		}

		opts.Logger.Info(opts.localizer.MustLocalize("kafka.acl.grantPermissions.consumer.log.info.aclsCreated", localize.NewEntry("InstanceName", kafkaName)))
	}

	if opts.producer {

		aclBindTopicWrite := *kafkainstanceclient.NewAclBinding(
			kafkainstanceclient.ACLRESOURCETYPE_TOPIC,
			topicNameArg,
			topicPatternArg,
			userArg,
			kafkainstanceclient.ACLOPERATION_WRITE,
			kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW,
		)

		req = req.AclBinding(aclBindTopicWrite)

		err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
		if err != nil {
			return err
		}

		aclBindTopicCreate := *kafkainstanceclient.NewAclBinding(
			kafkainstanceclient.ACLRESOURCETYPE_TOPIC,
			topicNameArg,
			topicPatternArg,
			userArg,
			kafkainstanceclient.ACLOPERATION_CREATE,
			kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW,
		)

		req = req.AclBinding(aclBindTopicCreate)

		err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
		if err != nil {
			return err
		}

		// Add ACLs for transactional IDs
		aclBindTransactionIDWrite := *kafkainstanceclient.NewAclBinding(
			kafkainstanceclient.ACLRESOURCETYPE_TRANSACTIONAL_ID,
			acl.Wildcard,
			kafkainstanceclient.ACLPATTERNTYPE_LITERAL,
			userArg,
			kafkainstanceclient.ACLOPERATION_WRITE,
			kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW,
		)

		req = req.AclBinding(aclBindTransactionIDWrite)

		err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
		if err != nil {
			return err
		}

		aclBindTransactionIDDescribe := *kafkainstanceclient.NewAclBinding(
			kafkainstanceclient.ACLRESOURCETYPE_TRANSACTIONAL_ID,
			acl.Wildcard,
			kafkainstanceclient.ACLPATTERNTYPE_LITERAL,
			userArg,
			kafkainstanceclient.ACLOPERATION_DESCRIBE,
			kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW,
		)

		req = req.AclBinding(aclBindTransactionIDDescribe)

		err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
		if err != nil {
			return err
		}

		opts.Logger.Info(opts.localizer.MustLocalize("kafka.acl.grantPermissions.producer.log.info.aclsCreated", localize.NewEntry("InstanceName", kafkaName)))
	}

	if opts.admin {

		aclBindClusterAlter := *kafkainstanceclient.NewAclBinding(
			kafkainstanceclient.ACLRESOURCETYPE_CLUSTER,
			acl.KafkaCluster,
			kafkainstanceclient.ACLPATTERNTYPE_LITERAL,
			userArg,
			kafkainstanceclient.ACLOPERATION_ALTER,
			kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW,
		)

		req = req.AclBinding(aclBindClusterAlter)

		err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
		if err != nil {
			return err
		}

		opts.Logger.Info(opts.localizer.MustLocalize("kafka.acl.grantPermissions.admin.log.info.aclsCreated", localize.NewEntry("InstanceName", kafkaName)))

	}

	return nil

}

func buildPrincipal(user string) string {
	return fmt.Sprintf("User:%s", user)
}
