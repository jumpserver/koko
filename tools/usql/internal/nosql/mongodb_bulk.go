package nosql

import (
	"fmt"
	"strings"

	"github.com/dop251/goja"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func bulkCollation(document bson.D) *options.Collation {
	if len(document) == 0 {
		return nil
	}
	return &options.Collation{Locale: fmt.Sprint(docGet(document, "locale")), CaseLevel: docGet(document, "caseLevel") == true, CaseFirst: stringValue(docGet(document, "caseFirst")), Strength: int(numeric(docGet(document, "strength"))), NumericOrdering: docGet(document, "numericOrdering") == true, Alternate: stringValue(docGet(document, "alternate")), MaxVariable: stringValue(docGet(document, "maxVariable")), Normalization: docGet(document, "normalization") == true, Backwards: docGet(document, "backwards") == true}
}
func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
func (h *mongoHandle) bulkWrite(call goja.FunctionCall) goja.Value {
	s := h.s
	operations, ok := s.toBSON(call.Argument(0)).(bson.A)
	if !ok || len(operations) == 0 {
		panic(s.vm.NewTypeError("bulkWrite requires a nonempty operation array"))
	}
	models := make([]mongo.WriteModel, 0, len(operations))
	for _, operation := range operations {
		doc := asDocument(operation)
		if len(doc) != 1 {
			panic(s.vm.NewTypeError("each bulk operation requires exactly one operation name"))
		}
		body := asDocument(doc[0].Value)
		if body == nil {
			panic(s.vm.NewTypeError("bulk operation body must be a document"))
		}
		allowed := map[string]string{
			"insertOne": " document ", "deleteOne": " filter hint collation ", "deleteMany": " filter hint collation ",
			"replaceOne": " filter replacement upsert hint collation ", "updateOne": " filter update upsert hint collation arrayFilters sort ", "updateMany": " filter update upsert hint collation arrayFilters ",
		}[doc[0].Key]
		for _, field := range body {
			if !strings.Contains(allowed, " "+field.Key+" ") {
				panic(s.vm.NewTypeError("unsupported bulk model field " + field.Key))
			}
		}
		filter := docGet(body, "filter")
		if doc[0].Key != "insertOne" {
			if _, ok := filter.(bson.D); !ok {
				panic(s.vm.NewTypeError("bulk update/delete/replace requires a filter document"))
			}
		}
		collation := bulkCollation(asDocument(docGet(body, "collation")))
		switch doc[0].Key {
		case "insertOne":
			document := docGet(body, "document")
			if _, ok := document.(bson.D); !ok {
				panic(s.vm.NewTypeError("bulk insertOne requires a document"))
			}
			models = append(models, mongo.NewInsertOneModel().SetDocument(document))
		case "deleteOne":
			model := mongo.NewDeleteOneModel().SetFilter(filter)
			if hint := docGet(body, "hint"); hint != nil {
				model.SetHint(hint)
			}
			if collation != nil {
				model.SetCollation(collation)
			}
			models = append(models, model)
		case "deleteMany":
			model := mongo.NewDeleteManyModel().SetFilter(filter)
			if hint := docGet(body, "hint"); hint != nil {
				model.SetHint(hint)
			}
			if collation != nil {
				model.SetCollation(collation)
			}
			models = append(models, model)
		case "replaceOne":
			model := mongo.NewReplaceOneModel().SetFilter(filter).SetReplacement(docGet(body, "replacement"))
			if value, ok := docGet(body, "upsert").(bool); ok {
				model.SetUpsert(value)
			}
			if hint := docGet(body, "hint"); hint != nil {
				model.SetHint(hint)
			}
			if collation != nil {
				model.SetCollation(collation)
			}
			models = append(models, model)
		case "updateOne":
			model := mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(docGet(body, "update"))
			if value, ok := docGet(body, "upsert").(bool); ok {
				model.SetUpsert(value)
			}
			if hint := docGet(body, "hint"); hint != nil {
				model.SetHint(hint)
			}
			if collation != nil {
				model.SetCollation(collation)
			}
			if values, ok := docGet(body, "arrayFilters").(bson.A); ok {
				model.SetArrayFilters([]any(values))
			}
			if sort := docGet(body, "sort"); sort != nil {
				model.SetSort(sort)
			}
			models = append(models, model)
		case "updateMany":
			model := mongo.NewUpdateManyModel().SetFilter(filter).SetUpdate(docGet(body, "update"))
			if value, ok := docGet(body, "upsert").(bool); ok {
				model.SetUpsert(value)
			}
			if hint := docGet(body, "hint"); hint != nil {
				model.SetHint(hint)
			}
			if collation != nil {
				model.SetCollation(collation)
			}
			if values, ok := docGet(body, "arrayFilters").(bson.A); ok {
				model.SetArrayFilters([]any(values))
			}
			models = append(models, model)
		default:
			panic(s.vm.NewTypeError("unknown bulk operation " + doc[0].Key))
		}
	}
	opts := options.BulkWrite()
	for _, e := range s.document(call.Argument(1)) {
		switch e.Key {
		case "ordered":
			opts.SetOrdered(e.Value == true)
		case "bypassDocumentValidation":
			opts.SetBypassDocumentValidation(e.Value == true)
		case "comment":
			opts.SetComment(e.Value)
		case "let":
			opts.SetLet(e.Value)
		default:
			panic(s.vm.NewTypeError("unsupported bulk option " + e.Key))
		}
	}
	result, err := h.client().Database(h.database).Collection(h.collection).BulkWrite(h.context(), models, opts)
	s.must(err)
	ids := bson.D{}
	for index, id := range result.UpsertedIDs {
		ids = append(ids, bson.E{Key: fmt.Sprint(index), Value: id})
	}
	return s.fromBSON(bson.D{{Key: "acknowledged", Value: result.Acknowledged}, {Key: "insertedCount", Value: result.InsertedCount}, {Key: "matchedCount", Value: result.MatchedCount}, {Key: "modifiedCount", Value: result.ModifiedCount}, {Key: "deletedCount", Value: result.DeletedCount}, {Key: "upsertedCount", Value: result.UpsertedCount}, {Key: "upsertedIds", Value: ids}})
}
func (h *mongoHandle) bulkBuilder(ordered bool) goja.Value {
	s := h.s
	object := s.vm.NewObject()
	operations := bson.A{}
	executed := false
	check := func() {
		if executed {
			panic(s.vm.NewTypeError("bulk operation has already executed"))
		}
	}
	_ = object.Set("insert", func(call goja.FunctionCall) goja.Value {
		check()
		operations = append(operations, bson.D{{Key: "insertOne", Value: bson.D{{Key: "document", Value: s.requiredDocument(call.Argument(0), "bulk insert")}}}})
		return object
	})
	_ = object.Set("find", func(call goja.FunctionCall) goja.Value {
		check()
		filter := s.requiredDocument(call.Argument(0), "bulk find")
		opts := bson.D{}
		finder := s.vm.NewObject()
		_ = finder.Set("upsert", func(goja.FunctionCall) goja.Value { check(); opts = docSet(opts, "upsert", true); return finder })
		for _, key := range []string{"hint", "collation", "arrayFilters"} {
			key := key
			_ = finder.Set(key, func(call goja.FunctionCall) goja.Value {
				check()
				opts = docSet(opts, key, s.toBSON(call.Argument(0)))
				return finder
			})
		}
		for method, operation := range map[string]string{"updateOne": "updateOne", "update": "updateMany", "replaceOne": "replaceOne", "remove": "deleteMany", "removeOne": "deleteOne"} {
			operation := operation
			_ = finder.Set(method, func(call goja.FunctionCall) goja.Value {
				check()
				body := append(bson.D{{Key: "filter", Value: filter}}, opts...)
				if operation == "replaceOne" {
					body = append(body, bson.E{Key: "replacement", Value: s.document(call.Argument(0))})
				} else if operation == "updateOne" || operation == "updateMany" {
					body = append(body, bson.E{Key: "update", Value: s.toBSON(call.Argument(0))})
				}
				operations = append(operations, bson.D{{Key: operation, Value: body}})
				return object
			})
		}
		return finder
	})
	_ = object.Set("execute", func(call goja.FunctionCall) goja.Value {
		check()
		executed = true
		opts := docSet(s.document(call.Argument(0)), "ordered", ordered)
		return h.bulkWrite(goja.FunctionCall{Arguments: []goja.Value{s.fromBSON(operations), s.fromBSON(opts)}})
	})
	_ = object.Set("getOperations", func(goja.FunctionCall) goja.Value { return s.fromBSON(operations) })
	_ = object.Set("toJSON", func(goja.FunctionCall) goja.Value {
		return s.fromBSON(bson.D{{Key: "nOperations", Value: len(operations)}})
	})
	return object
}
