package mongobatch

import (
	"encoding/hex"
	"fmt"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"reflect"
)

// FetchInput fetches defined lengths of batches from a Mongo collection.
// The retrieved batch will be marked processing. If a record is not marked
// as "processed" within a given time interval, the record will be reverted
// back to the initial non-processed state. This workflow supports concurrent processing.

// The conf argument is a Configuration object initialized with values necessary for
// the mongo connection.

// The result argument must be the address for a slice. It will hold the resulting result set.

// For instance:
//
//    var result []struct{ Value int }
//	  config := NewConfiguration("localhost", 27017, "salaries", "batch")
//    err := FetchInput(config, &results)
//    if err != nil {
//        return err
//    }
//
func FetchInput(conf *Configuration, result interface{}) error {
	//TODO
	//use a distributed lock for mutual exclusion
	session, err := mgo.Dial(connectString(conf))
	if err != nil {
		return err
	}
	defer session.Close()
	// query db
	c := session.DB(conf.Database).C(conf.Collection)
	iter := c.Find(conf.FetchQuery).Limit(conf.FetchLimit).Sort(conf.FetchOrder).Iter()
	if err = iter.All(result); err != nil {
		return err
	}
	ids := fetchIds(conf, result)
	fmt.Println(ids)
	// update all matching documents to processing
	_, err = c.UpdateAll(bson.M{"_id": bson.M{"$in": ids}}, bson.M{"$set": bson.M{"state": "processing"}})

	return err
}

// fetchIds returns the list of IDs contained in the result
func fetchIds(conf *Configuration, result interface{}) []string {
	resultv := reflect.ValueOf(result)
	if resultv.Kind() != reflect.Ptr || resultv.Elem().Kind() != reflect.Slice {
		panic("result argument must be a slice address")
	}
	slicev := resultv.Elem()
	slicev = slicev.Slice(0, slicev.Cap())
	elemt := slicev.Type().Elem()
	if elemt.Kind() != reflect.Struct {
		panic("result slice's type should be struct")
	}
	fld, ok := elemt.FieldByName("Id")
	if !ok {
		panic("result slice's elements should have an ID field")
	}
	if fld.Type.String() != "bson.ObjectId" {
		panic("ID field should be of type bson.ObjectId")
	}
	ids := make([]string, slicev.Len())
	for i := 0; i < slicev.Len(); i++ {
		ids[i] = hex.EncodeToString([]byte(slicev.Index(i).FieldByName("Id").String()))
	}

	return ids
}

// markProcessing updates the state of records in the iter to "processing"
// and stores records in the result slice.
func markProcessing(conf *Configuration, iter *mgo.Iter, result interface{}) error {
	resultv := reflect.ValueOf(result)
	if resultv.Kind() != reflect.Ptr || resultv.Elem().Kind() != reflect.Slice {
		panic("result argument must be a slice address")
	}
	slicev := resultv.Elem()
	slicev = slicev.Slice(0, slicev.Cap())
	elemt := slicev.Type().Elem()
	if elemt.Kind() != reflect.Struct {
		panic("result slice's type should be struct")
	}
	fld, ok := elemt.FieldByName(conf.StateFld)
	if !ok {
		panic("result slice's elements should have a state field as defined by configuration.StateFld")
	}
	if fld.Type.Kind() != reflect.String {
		panic("result struct's state should be a string")
	}

	i := 0
	for {
		if slicev.Len() == i {
			elemp := reflect.New(elemt)
			if !iter.Next(elemp.Interface()) {
				break
			}
			slicev = reflect.Append(slicev, elemp.Elem())
			slicev = slicev.Slice(0, slicev.Cap())
		} else {
			if !iter.Next(slicev.Index(i).Addr().Interface()) {
				break
			}
		}
		slicev.Index(i).FieldByName(conf.StateFld).SetString("processing")
		i++
	}
	resultv.Elem().Set(slicev.Slice(0, i))
	return iter.Close()
}
