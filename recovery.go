package main

import (
	"fmt"
	"log"

	"github.com/coreos/etcd/clientv3"
	"golang.org/x/net/context"
)

// Definitions:
// - running pods P in k8s cluster
// - membership M in controller knowledge
// Steps:
// 1. Remove all pods from set P that does not belong to set M
// 2. P’ consist of remaining pods of P
// 3. If P’ = M, the current state matches the membership state. END.
// 4. If len(P’) < len(M)/2 + 1, quorum lost. Go to recovery process (TODO).
// 5. Add one missing member. END.
func (c *Cluster) reconcile(P, M MemberSet) error {
	log.Println("Reconciling:")
	log.Println("Running pods:", P)
	log.Println("Expected membership:", M)

	defer func() {
		log.Println("Finish Reconciling\n")
	}()

	// update members
	// TODO: move this out of reconcile
	c.members = M

	unknownMembers := P.Diff(c.members)
	if unknownMembers.Size() > 0 {
		log.Println("Removing unexpected pods:", unknownMembers)
		for _, m := range unknownMembers {
			if err := c.removePodAndService(m.Name); err != nil {
				return err
			}
		}
	}
	L := P.Diff(unknownMembers)
	if L.Size() == c.members.Size() {
		fmt.Println("Match")
		return nil
	}

	if L.Size() < c.members.Size()/2+1 {
		fmt.Println("Disaster recovery")
		return c.disasterRecovery()
	}

	fmt.Println("Recovery one member")
	toRecover := c.members.Diff(L).PickOne()
	return c.recoverOneMember(toRecover)
}

func (c *Cluster) recoverOneMember(toRecover Member) error {
	// Remove toRecover membership first since it's gone
	cfg := clientv3.Config{
		Endpoints: []string{makeClientAddr(c.members.PickOne().Name)},
	}
	etcdcli, err := clientv3.New(cfg)
	if err != nil {
		return err
	}

	_, err = etcdcli.MemberRemove(context.TODO(), toRecover.ID)
	if err != nil {
		return err
	}
	log.Printf("removed member (%v) with ID (%d)\n", toRecover.Name, toRecover.ID)
	c.members.Remove(toRecover.Name)

	// Add a new member
	newMember := fmt.Sprintf("%s-%04d", c.name, c.idCounter)
	resp, err := etcdcli.MemberAdd(context.TODO(), []string{makeEtcdPeerAddr(newMember)})
	if err != nil {
		panic(err)
	}
	c.idCounter++
	c.members.Add(Member{Name: resp.Member.Name, ID: resp.Member.ID})

	initialCluster := c.members.PeerURLPairs()
	if err := c.createPodAndService(c.idCounter, initialCluster, "existing"); err != nil {
		return err
	}
	log.Printf("added member, cluster: %s", initialCluster)
	return nil
}

func (c *Cluster) disasterRecovery() error {
	panic("unimplemented disaster recovery")
}