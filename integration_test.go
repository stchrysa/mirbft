/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package mirbft

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	pb "github.com/IBM/mirbft/mirbftpb"

	"go.uber.org/zap"
)

var _ = Describe("Integration", func() {
	var (
		serializer      *serializer
		stateMachineVal *stateMachine
		epochConfig     *pb.EpochConfig
		networkConfig   *pb.NetworkConfig
		consumerConfig  *Config
		logger          *zap.Logger

		doneC chan struct{}
	)

	BeforeEach(func() {
		var err error
		logger, err = zap.NewDevelopment()
		Expect(err).NotTo(HaveOccurred())

		consumerConfig = &Config{
			ID:     0,
			Logger: logger,
			BatchParameters: BatchParameters{
				CutSizeBytes: 1,
			},
		}

		doneC = make(chan struct{})
	})

	AfterEach(func() {
		logger.Sync()
		close(doneC)
	})

	Describe("F=0,N=1", func() {
		BeforeEach(func() {
			epochConfig = &pb.EpochConfig{
				Number:             3,
				Leaders:            []uint64{0},
				StartingCheckpoint: &pb.Checkpoint{},
			}

			networkConfig = &pb.NetworkConfig{
				CheckpointInterval: 2,
				F:                  0,
				Nodes:              []uint64{0},
				NumberOfBuckets:    1,
				MaxEpochLength:     10,
			}

			stateMachineVal = newStateMachine(networkConfig, consumerConfig)
			stateMachineVal.activeEpoch = newEpoch(epochConfig, stateMachineVal.checkpointTracker, stateMachineVal.clientWindows, nil, networkConfig, consumerConfig)
			stateMachineVal.nodeMsgs[0].setActiveEpoch(stateMachineVal.activeEpoch)

			serializer = newSerializer(stateMachineVal, doneC)
		})

		It("works from proposal through commit", func() {
			By("proposing a message")
			serializer.propC <- &pb.RequestData{
				ClientId:  []byte("client-1"),
				ReqNo:     1,
				Data:      []byte("data"),
				Signature: []byte("signature"),
			}
			actions := &Actions{}
			Eventually(serializer.actionsC).Should(Receive(actions))
			Expect(actions).To(Equal(&Actions{
				Preprocess: []*Request{
					{
						Source: 0,
						ClientRequest: &pb.RequestData{
							ClientId:  []byte("client-1"),
							ReqNo:     1,
							Data:      []byte("data"),
							Signature: []byte("signature"),
						},
					},
				},
			}))

			By("returning a processed version of the proposal")
			serializer.resultsC <- ActionResults{
				Preprocessed: []*PreprocessResult{
					{
						Digest: uint64ToBytes(7),
						RequestData: &pb.RequestData{
							ClientId:  []byte("client-1"),
							ReqNo:     1,
							Data:      []byte("data"),
							Signature: []byte("signature"),
						},
					},
				},
			}

			Eventually(serializer.actionsC).Should(Receive(actions))
			Expect(actions).To(Equal(&Actions{
				Process: []*Batch{
					{
						Epoch: 3,
						SeqNo: 1,
						Requests: []*PreprocessResult{
							{
								Digest: uint64ToBytes(7),
								RequestData: &pb.RequestData{
									ClientId:  []byte("client-1"),
									ReqNo:     1,
									Data:      []byte("data"),
									Signature: []byte("signature"),
								},
							},
						},
					},
				},
			}))

			By("returning a the process result for the batch")
			serializer.resultsC <- ActionResults{
				Processed: []*ProcessResult{
					{
						Epoch:  3,
						SeqNo:  1,
						Digest: []byte("fake-digest"),
					},
				},
			}
			Eventually(serializer.actionsC).Should(Receive(actions))
			Expect(actions).To(Equal(&Actions{
				Broadcast: []*pb.Msg{
					{
						Type: &pb.Msg_Forward{
							Forward: &pb.Forward{
								RequestData: &pb.RequestData{
									ClientId:  []byte("client-1"),
									ReqNo:     1,
									Data:      []byte("data"),
									Signature: []byte("signature"),
								},
							},
						},
					},
					{
						Type: &pb.Msg_Preprepare{
							Preprepare: &pb.Preprepare{
								Epoch: 3,
								SeqNo: 1,
								Batch: []*pb.Request{
									{
										ClientId: []byte("client-1"),
										ReqNo:    1,
										Digest:   uint64ToBytes(7),
									},
								},
							},
						},
					},
				},
				QEntries: []*pb.QEntry{
					{
						Epoch:  3,
						SeqNo:  1,
						Digest: []byte("fake-digest"),
						Requests: []*pb.Request{
							{
								ClientId: []byte("client-1"),
								ReqNo:    1,
								Digest:   uint64ToBytes(7),
							},
						},
					},
				},
			}))

			By("broadcasting the pre-prepare to myself")
			serializer.stepC <- step{
				Source: 0,
				Msg:    actions.Broadcast[1],
			}
			Eventually(serializer.actionsC).Should(Receive(actions))
			Expect(actions).To(Equal(&Actions{
				Broadcast: []*pb.Msg{
					{
						Type: &pb.Msg_Commit{
							Commit: &pb.Commit{
								Epoch:  3,
								SeqNo:  1,
								Digest: []byte("fake-digest"),
							},
						},
					},
				},
				PEntries: []*pb.PEntry{
					{
						Epoch:  3,
						SeqNo:  1,
						Digest: []byte("fake-digest"),
					},
				},
			}))

			By("broadcasting the commit to myself")
			serializer.stepC <- step{
				Source: 0,
				Msg:    actions.Broadcast[0],
			}
			Eventually(serializer.actionsC).Should(Receive(actions))
			Expect(actions).To(Equal(&Actions{
				Commits: []*Commit{
					{
						QEntry: &pb.QEntry{
							Epoch:  3,
							SeqNo:  1,
							Digest: []byte("fake-digest"),
							Requests: []*pb.Request{
								{
									ClientId: []byte("client-1"),
									ReqNo:    1,
									Digest:   uint64ToBytes(7),
								},
							},
						},
					},
				},
			}))
		})
	})

	Describe("F=1,N=4", func() {
		BeforeEach(func() {
			epochConfig = &pb.EpochConfig{
				Number:             3,
				Leaders:            []uint64{0, 1, 2, 3},
				StartingCheckpoint: &pb.Checkpoint{},
			}

			networkConfig = &pb.NetworkConfig{
				CheckpointInterval: 5,
				F:                  1,
				Nodes:              []uint64{0, 1, 2, 3},
				NumberOfBuckets:    4,
				MaxEpochLength:     10,
			}

			stateMachineVal = newStateMachine(networkConfig, consumerConfig)
			stateMachineVal.activeEpoch = newEpoch(epochConfig, stateMachineVal.checkpointTracker, stateMachineVal.clientWindows, nil, networkConfig, consumerConfig)
			stateMachineVal.nodeMsgs[0].setActiveEpoch(stateMachineVal.activeEpoch)
			stateMachineVal.nodeMsgs[1].setActiveEpoch(stateMachineVal.activeEpoch)
			stateMachineVal.nodeMsgs[2].setActiveEpoch(stateMachineVal.activeEpoch)
			stateMachineVal.nodeMsgs[3].setActiveEpoch(stateMachineVal.activeEpoch)

			serializer = newSerializer(stateMachineVal, doneC)

		})

		It("works from proposal through commit", func() {
			By("proposing a message")
			serializer.propC <- &pb.RequestData{
				ClientId:  []byte("client-1"),
				ReqNo:     1,
				Data:      []byte("data"),
				Signature: []byte("signature"),
			}
			actions := &Actions{}
			Eventually(serializer.actionsC).Should(Receive(actions))
			Expect(actions).To(Equal(&Actions{
				Preprocess: []*Request{
					{
						Source: 0,
						ClientRequest: &pb.RequestData{
							ClientId:  []byte("client-1"),
							ReqNo:     1,
							Data:      []byte("data"),
							Signature: []byte("signature"),
						},
					},
				},
			}))

			By("returning a processed version of the proposal")
			serializer.resultsC <- ActionResults{
				Preprocessed: []*PreprocessResult{
					{
						Digest: uint64ToBytes(7),
						RequestData: &pb.RequestData{
							ClientId:  []byte("client-1"),
							ReqNo:     1,
							Data:      []byte("data"),
							Signature: []byte("signature"),
						},
					},
				},
			}

			// TODO, we should include this, and make sure that we don't reprocess
			// once we include the expected digest on the forward
			/*
				By("faking a forward from the leader")
				serializer.stepC <- step{
					Source: 3,
					Msg: &pb.Msg{
						Type: &pb.Msg_Forward{
							Forward: &pb.Forward{
								RequestData: &pb.RequestData{
									ClientId:  []byte("client-1"),
									ReqNo:     1,
									Data:      []byte("data"),
									Signature: []byte("signature"),
								},
							},
						},
					},
				}
			*/

			By("faking a preprepare from the leader")
			serializer.stepC <- step{
				Source: 3,
				Msg: &pb.Msg{
					Type: &pb.Msg_Preprepare{
						Preprepare: &pb.Preprepare{
							Epoch: 3,
							SeqNo: 1,
							Batch: []*pb.Request{
								{
									ClientId: []byte("client-1"),
									ReqNo:    1,
									Digest:   uint64ToBytes(7),
								},
							},
						},
					},
				},
			}
			Eventually(serializer.actionsC).Should(Receive(actions))
			Expect(actions).To(Equal(&Actions{
				Process: []*Batch{
					{
						Source: 3,
						Epoch:  3,
						SeqNo:  1,
						Requests: []*PreprocessResult{
							{
								Digest: uint64ToBytes(7),
								RequestData: &pb.RequestData{
									ClientId:  []byte("client-1"),
									ReqNo:     1,
									Data:      []byte("data"),
									Signature: []byte("signature"),
								},
							},
						},
					},
				},
			}))

			By("returning a digest for the batch")
			serializer.resultsC <- ActionResults{
				Processed: []*ProcessResult{
					{
						Epoch:  3,
						SeqNo:  1,
						Digest: []byte("fake-digest"),
					},
				},
			}
			Eventually(serializer.actionsC).Should(Receive(actions))
			Expect(actions).To(Equal(&Actions{
				Broadcast: []*pb.Msg{
					{
						Type: &pb.Msg_Prepare{
							Prepare: &pb.Prepare{
								Epoch:  3,
								SeqNo:  1,
								Digest: []byte(("fake-digest")),
							},
						},
					},
				},
				QEntries: []*pb.QEntry{
					{
						Epoch:  3,
						SeqNo:  1,
						Digest: []byte("fake-digest"),
						Requests: []*pb.Request{
							{
								ClientId: []byte("client-1"),
								ReqNo:    1,
								Digest:   uint64ToBytes(7),
							},
						},
					},
				},
			}))

			By("broadcasting the prepare to myself, and from one other node")
			serializer.stepC <- step{
				Source: 0,
				Msg:    actions.Broadcast[0],
			}

			serializer.stepC <- step{
				Source: 1,
				Msg:    actions.Broadcast[0],
			}

			Eventually(serializer.actionsC).Should(Receive(actions))
			Expect(actions).To(Equal(&Actions{
				Broadcast: []*pb.Msg{
					{
						Type: &pb.Msg_Commit{
							Commit: &pb.Commit{
								Epoch:  3,
								SeqNo:  1,
								Digest: []byte(("fake-digest")),
							},
						},
					},
				},
				PEntries: []*pb.PEntry{
					{
						Epoch:  3,
						SeqNo:  1,
						Digest: []byte("fake-digest"),
					},
				},
			}))

			By("broadcasting the commit to myself, and from two other nodes")
			serializer.stepC <- step{
				Source: 0,
				Msg:    actions.Broadcast[0],
			}

			serializer.stepC <- step{
				Source: 1,
				Msg:    actions.Broadcast[0],
			}

			serializer.stepC <- step{
				Source: 3,
				Msg:    actions.Broadcast[0],
			}

			Eventually(serializer.actionsC).Should(Receive(actions))
			Expect(actions).To(Equal(&Actions{
				Commits: []*Commit{
					{
						QEntry: &pb.QEntry{
							Epoch:  3,
							SeqNo:  1,
							Digest: []byte("fake-digest"),
							Requests: []*pb.Request{
								{
									ClientId: []byte("client-1"),
									ReqNo:    1,
									Digest:   uint64ToBytes(7),
								},
							},
						},
					},
				},
			}))
		})
	})
})
