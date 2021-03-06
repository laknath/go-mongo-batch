package mongobatch

import (
	"github.com/laknath/go-mongo-batch/internal"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"log"
	"reflect"
	"time"
)

// BufferBatch returns a buffered channel of length bufsize
// that will stream fetched objects capped at bufsize.
// This is a convenience method for FetchBatch.
func BufferBatch(conf *Configuration, result interface{}, bufsize int) chan interface{} {
	elemt := internal.VerifyStructPointer(result)
	c := make(chan interface{}, bufsize)

	go func() {
		for {
			// create an addressable pointer to a new slice
			slicep := internal.NewPointerToSlice(elemt.Elem())
			err := FetchBatch(conf, slicep.Interface())

			if err != nil {
				log.Println(err)
				time.Sleep(time.Duration(conf.ErrorSleep) * time.Millisecond)
				continue
			}

			slicev := slicep.Elem()
			for i := 0; i < slicev.Len(); i++ {
				c <- slicev.Index(i).Addr().Interface()
			}

			// if no records fetched, wait and retry
			if slicev.Len() == 0 {
				// TODO add an exponential backoff
				time.Sleep(time.Duration(conf.NoRecordSleep) * time.Millisecond)
			}
		}
	}()

	return c
}

// FetchInput fetches defined lengths of batches from a Mongo collection.
// The retrieved batch will be marked processing. If a record is not marked
// as "processed" within a given time interval, the record will be reverted
// back to the initial non-processed state. This workflow supports concurrent processing.

// The conf argument is a Configuration object initialized with values necessary for
// the mongo connection.

// The results argument must be the address for a slice. It will hold the resulting result set.

// For instance:
//
//    var results []struct{ Value int }
//	  config := NewConfiguration("localhost", 27017, "salaries", "batch")
//    err := FetchInput(config, &results)
//    if err != nil {
//        return err
//    }
//
func FetchBatch(conf *Configuration, results interface{}) error {
	internal.VerifySlice(results)

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
	if err = iter.All(results); err != nil {
		return err
	}
	slicev := reflect.ValueOf(results).Elem()
	ids := fetchIds(conf, slicev)
	// update all matching documents to processing
	_, err = c.UpdateAll(
		bson.M{"_id": bson.M{"$in": ids}},
		bson.M{"$set": bson.M{conf.StateFld: conf.ProcessingState, conf.ProcessingTimeFld: time.Now().Unix()}},
	)

	return err
}

// fetchIds returns the list of IDs contained in slicev.
func fetchIds(conf *Configuration, slicev reflect.Value) []bson.ObjectId {
	ids := make([]bson.ObjectId, slicev.Len())
	for i := 0; i < slicev.Len(); i++ {
		//TODO avoid double conversion by fixing interface conversion: interface {} panic
		var e reflect.Value
		if slicev.Index(i).Kind() == reflect.Ptr {
			e = slicev.Index(i).Elem().FieldByName("Id")
		} else {
			e = slicev.Index(i).FieldByName("Id")
		}
		ids[i] = internal.ObjectIdFromString(e.String())
	}

	return ids
}
