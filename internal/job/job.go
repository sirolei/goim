package job

import (
	"context"
	"fmt"
	"github.com/Shopify/sarama"
	"google.golang.org/protobuf/proto"
	"sync"
	"time"

	"github.com/bilibili/discovery/naming"
	pb "goim/api/logic"
	"goim/internal/job/conf"

	log "github.com/golang/glog"
)

// Job is push job.
type Job struct {
	c            *conf.Config
	consumer     *sarama.ConsumerGroup
	cometServers map[string]*Comet

	rooms      map[string]*Room
	roomsMutex sync.RWMutex
}

// New new a push job.
func New(c *conf.Config) *Job {
	j := &Job{
		c:        c,
		consumer: newKafkaSub(c.Kafka),
		rooms:    make(map[string]*Room),
	}
	j.watchComet(c.Discovery)
	return j
}

func newKafkaSub(c *conf.Kafka) *sarama.ConsumerGroup {
	config := sarama.NewConfig()
	config.Consumer.Return.Errors = true
	config.Version = sarama.V2_0_0_0
	consumer, err := sarama.NewConsumerGroup(c.Brokers, c.Group, config)
	if err != nil {
		panic(err)
	}
	return &consumer
}

// Close close resounces.
func (j *Job) Close() error {
	if j.consumer != nil {
		return (*j.consumer).Close()
	}
	return nil
}

type jobConsumerGroupHandler struct {
	job *Job
}

func (h *jobConsumerGroupHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *jobConsumerGroupHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }
func (h *jobConsumerGroupHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		fmt.Printf("Message topic:%q partition:%d offset:%d\n", msg.Topic, msg.Partition, msg.Offset)
		sess.MarkMessage(msg, "")
		pushMsg := new(pb.PushMsg)
		if err := proto.Unmarshal(msg.Value, pushMsg); err != nil {
			log.Errorf("proto.Unmarshal(%v) error(%v)", msg, err)
			continue
		}
		if err := h.job.push(context.Background(), pushMsg); err != nil {
			log.Errorf("j.push(%v) error(%v)", pushMsg, err)
		}
		log.Infof("consume: %s/%d/%d\t%s\t%+v", msg.Topic, msg.Partition, msg.Offset, msg.Key, pushMsg)
	}
	return nil
}

// Consume messages, watch signals
func (j *Job) Consume() {

	consumerGroup := *j.consumer

	defer func() { _ = consumerGroup.Close() }()

	go func() {
		for err := range consumerGroup.Errors() {
			log.Errorf("consumer error(%v)", err)
		}
	}()

	ctx := context.Background()
	topics := []string{j.c.Kafka.Topic}
	for {
		handler := &jobConsumerGroupHandler{job: j}
		err := consumerGroup.Consume(ctx, topics, handler)
		if err != nil {
			panic(err)
		}
	}

	//
	//for {
	//	select {
	//	case err := <-consumerGroup.Errors():
	//		log.Errorf("consumer error(%v)", err)
	//
	//	case
	//
	//	case msg, ok := <-j.consumer.Messages():
	//		if !ok {
	//			return
	//		}
	//		j.consumer.MarkOffset(msg, "")
	//		// process push message
	//		pushMsg := new(pb.PushMsg)
	//		if err := proto.Unmarshal(msg.Value, pushMsg); err != nil {
	//			log.Errorf("proto.Unmarshal(%v) error(%v)", msg, err)
	//			continue
	//		}
	//		if err := j.push(context.Background(), pushMsg); err != nil {
	//			log.Errorf("j.push(%v) error(%v)", pushMsg, err)
	//		}
	//		log.Infof("consume: %s/%d/%d\t%s\t%+v", msg.Topic, msg.Partition, msg.Offset, msg.Key, pushMsg)
	//	}
	//}
}

func (j *Job) watchComet(c *naming.Config) {
	dis := naming.New(c)
	resolver := dis.Build("goim.comet")
	event := resolver.Watch()
	select {
	case _, ok := <-event:
		if !ok {
			panic("watchComet init failed")
		}
		if ins, ok := resolver.Fetch(); ok {
			if err := j.newAddress(ins.Instances); err != nil {
				panic(err)
			}
			log.Infof("watchComet init newAddress:%+v", ins)
		}
	case <-time.After(10 * time.Second):
		log.Error("watchComet init instances timeout")
	}
	go func() {
		for {
			if _, ok := <-event; !ok {
				log.Info("watchComet exit")
				return
			}
			ins, ok := resolver.Fetch()
			if ok {
				if err := j.newAddress(ins.Instances); err != nil {
					log.Errorf("watchComet newAddress(%+v) error(%+v)", ins, err)
					continue
				}
				log.Infof("watchComet change newAddress:%+v", ins)
			}
		}
	}()
}

func (j *Job) newAddress(insMap map[string][]*naming.Instance) error {
	ins := insMap[j.c.Env.Zone]
	if len(ins) == 0 {
		return fmt.Errorf("watchComet instance is empty")
	}
	comets := map[string]*Comet{}
	for _, in := range ins {
		if old, ok := j.cometServers[in.Hostname]; ok {
			comets[in.Hostname] = old
			continue
		}
		c, err := NewComet(in, j.c.Comet)
		if err != nil {
			log.Errorf("watchComet NewComet(%+v) error(%v)", in, err)
			return err
		}
		comets[in.Hostname] = c
		log.Infof("watchComet AddComet grpc:%+v", in)
	}
	for key, old := range j.cometServers {
		if _, ok := comets[key]; !ok {
			old.cancel()
			log.Infof("watchComet DelComet:%s", key)
		}
	}
	j.cometServers = comets
	return nil
}
