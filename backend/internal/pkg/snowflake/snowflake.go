package snowflake

import (
	sf "github.com/bwmarrin/snowflake"
)

var node *sf.Node

func Init(machineID int64) error {
	var err error
	node, err = sf.NewNode(machineID)
	return err
}

func GenID() int64 {
	return node.Generate().Int64()
}
