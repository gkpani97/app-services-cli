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

	kafkaID    string
	topic      string
	user       string
	svcAccount string
	group      string
	producer   bool
	consumer   bool
	prefix     bool
	admin      bool
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

			return runGrantPermissions(opts)
		},
	}

	cmd.Flags().StringVar(&opts.user, "user", "", opts.localizer.MustLocalize("kafka.acl.grantPermissions.flag.user.description"))
	cmd.Flags().StringVar(&opts.svcAccount, "service-account", "", opts.localizer.MustLocalize("kafka.acl.grantPermissions.flag.serviceAccount.description"))
	cmd.Flags().StringVar(&opts.topic, "topic", "", opts.localizer.MustLocalize("kafka.acl.grantPermissions.flag.topic.description"))
	cmd.Flags().StringVar(&opts.group, "group", "", opts.localizer.MustLocalize("kafka.acl.grantPermissions.flag.group.description"))
	cmd.Flags().BoolVar(&opts.consumer, "consumer", false, opts.localizer.MustLocalize("kafka.acl.grantPermissions.flag.consumer.description"))
	cmd.Flags().BoolVar(&opts.producer, "producer", false, opts.localizer.MustLocalize("kafka.acl.grantPermissions.flag.producer.description"))
	cmd.Flags().BoolVar(&opts.prefix, "prefix", false, opts.localizer.MustLocalize("kafka.acl.grantPermissions.flag.prefix.description"))
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

	var topicNameArg string = acl.Wildcard
	var groupIdArg string = acl.Wildcard
	var patternArg kafkainstanceclient.AclPatternType = kafkainstanceclient.ACLPATTERNTYPE_LITERAL

	var userArg string

	if opts.topic != "" {
		topicNameArg = opts.topic
	}

	if opts.prefix {
		patternArg = kafkainstanceclient.ACLPATTERNTYPE_PREFIXED
	}

	if opts.user != "" {
		userArg = buildPrincipal(opts.user)
	}

	if opts.svcAccount != "" {
		userArg = buildPrincipal(opts.svcAccount)
	}

	req := api.AclsApi.CreateAcl(opts.Context)

	if opts.producer || opts.consumer {
		aclBindTopicDescribe := *kafkainstanceclient.NewAclBinding(kafkainstanceclient.ACLRESOURCETYPE_TOPIC, topicNameArg, patternArg, userArg, kafkainstanceclient.ACLOPERATION_DESCRIBE, kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW)

		req = req.AclBinding(aclBindTopicDescribe)

		err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
		if err != nil {
			return err
		}
	}

	if opts.consumer {

		aclBindTopicRead := *kafkainstanceclient.NewAclBinding(kafkainstanceclient.ACLRESOURCETYPE_TOPIC, topicNameArg, patternArg, userArg, kafkainstanceclient.ACLOPERATION_READ, kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW)

		req = req.AclBinding(aclBindTopicRead)

		err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
		if err != nil {
			return err
		}

		aclBindGroupRead := *kafkainstanceclient.NewAclBinding(kafkainstanceclient.ACLRESOURCETYPE_GROUP, groupIdArg, patternArg, userArg, kafkainstanceclient.ACLOPERATION_READ, kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW)

		req = api.AclsApi.CreateAcl(opts.Context).AclBinding(aclBindGroupRead)

		err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
		if err != nil {
			return err
		}

		opts.Logger.Info(opts.localizer.MustLocalize("kafka.acl.grantPermissions.consumer.log.info.aclsCreated", localize.NewEntry("InstanceName", kafkaName)))
	}

	if opts.producer {

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

		// Add ACLs for transactional IDs
		aclBindTransactionIDWrite := *kafkainstanceclient.NewAclBinding(kafkainstanceclient.ACLRESOURCETYPE_TRANSACTIONAL_ID, acl.Wildcard, kafkainstanceclient.ACLPATTERNTYPE_LITERAL, userArg, kafkainstanceclient.ACLOPERATION_WRITE, kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW)

		req = req.AclBinding(aclBindTransactionIDWrite)

		err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
		if err != nil {
			return err
		}

		aclBindTransactionIDDescribe := *kafkainstanceclient.NewAclBinding(kafkainstanceclient.ACLRESOURCETYPE_TRANSACTIONAL_ID, acl.Wildcard, kafkainstanceclient.ACLPATTERNTYPE_LITERAL, userArg, kafkainstanceclient.ACLOPERATION_DESCRIBE, kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW)

		req = req.AclBinding(aclBindTransactionIDDescribe)

		err = acl.ExecuteACLRuleCreate(req, opts.localizer, kafkaName)
		if err != nil {
			return err
		}

		opts.Logger.Info(opts.localizer.MustLocalize("kafka.acl.grantPermissions.producer.log.info.aclsCreated", localize.NewEntry("InstanceName", kafkaName)))
	}

	if opts.admin {
		aclBindClusterAlter := *kafkainstanceclient.NewAclBinding(kafkainstanceclient.ACLRESOURCETYPE_CLUSTER, acl.KafkaCluster, kafkainstanceclient.ACLPATTERNTYPE_LITERAL, userArg, kafkainstanceclient.ACLOPERATION_ALTER, kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW)

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
