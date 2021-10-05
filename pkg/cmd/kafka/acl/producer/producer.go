package producer

import (
	"context"
	"fmt"
	"net/http"

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

	httpRes, err := req.Execute()
	if httpRes != nil {
		defer httpRes.Body.Close()
	}

	if err != nil {
		if httpRes == nil {
			return err
		}

		operationTmplPair := localize.NewEntry("Operation", "create")

		switch httpRes.StatusCode {
		case http.StatusUnauthorized:
			return opts.localizer.MustLocalizeError("kafka.acl.common.error.unauthorized", operationTmplPair)
		case http.StatusForbidden:
			return opts.localizer.MustLocalizeError("kafka.acl.common.error.forbidden", operationTmplPair)
		case http.StatusInternalServerError:
			return opts.localizer.MustLocalizeError("kafka.acl.common.error.internalServerError")
		case http.StatusServiceUnavailable:
			return opts.localizer.MustLocalizeError("kafka.acl.common.error.unableToConnectToKafka", localize.NewEntry("Name", kafkaInstance.GetName()))
		default:
			return err
		}
	}

	aclBindTopicWrite := *kafkainstanceclient.NewAclBinding(kafkainstanceclient.ACLRESOURCETYPE_TOPIC, topicNameArg, patternArg, userArg, kafkainstanceclient.ACLOPERATION_WRITE, kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW)

	req = req.AclBinding(aclBindTopicWrite)

	httpRes, err = req.Execute()
	if httpRes != nil {
		defer httpRes.Body.Close()
	}

	if err != nil {
		if httpRes == nil {
			return err
		}

		operationTmplPair := localize.NewEntry("Operation", "create")

		switch httpRes.StatusCode {
		case http.StatusUnauthorized:
			return opts.localizer.MustLocalizeError("kafka.acl.common.error.unauthorized", operationTmplPair)
		case http.StatusForbidden:
			return opts.localizer.MustLocalizeError("kafka.acl.common.error.forbidden", operationTmplPair)
		case http.StatusInternalServerError:
			return opts.localizer.MustLocalizeError("kafka.acl.common.error.internalServerError")
		case http.StatusServiceUnavailable:
			return opts.localizer.MustLocalizeError("kafka.acl.common.error.unableToConnectToKafka", localize.NewEntry("Name", kafkaInstance.GetName()))
		default:
			return err
		}
	}

	aclBindTopicCreate := *kafkainstanceclient.NewAclBinding(kafkainstanceclient.ACLRESOURCETYPE_TOPIC, topicNameArg, patternArg, userArg, kafkainstanceclient.ACLOPERATION_CREATE, kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW)

	req = req.AclBinding(aclBindTopicCreate)

	httpRes, err = req.Execute()
	if httpRes != nil {
		defer httpRes.Body.Close()
	}

	if opts.transactionID != "" {
		aclBindTransactionAll := *kafkainstanceclient.NewAclBinding(kafkainstanceclient.ACLRESOURCETYPE_TRANSACTIONAL_ID, opts.transactionID, kafkainstanceclient.ACLPATTERNTYPE_LITERAL, userArg, kafkainstanceclient.ACLOPERATION_ALL, kafkainstanceclient.ACLPERMISSIONTYPE_ALLOW)

		req = req.AclBinding(aclBindTransactionAll)

		httpRes, err = req.Execute()
		if httpRes != nil {
			defer httpRes.Body.Close()
		}
	}

	opts.Logger.Info(opts.localizer.MustLocalize("kafka.acl.producer.log.info.aclsCreated", localize.NewEntry("InstanceName", kafkaInstance.GetName())))

	return nil
}

func buildPrincipal(user string) string {
	return fmt.Sprintf("User:%s", user)
}
