package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

func runFlowWork(arguments []string) error {
	if len(arguments) == 0 {
		return fmt.Errorf("usage: boatstack flow work <show|input-required|answer|complete|block> [flags]")
	}
	action := arguments[0]
	operations := map[string]surfaces.Operation{
		"show": surfaces.OperationWorkShow, "input-required": surfaces.OperationWorkInputRequired,
		"answer": surfaces.OperationWorkAnswer, "complete": surfaces.OperationWorkComplete, "block": surfaces.OperationWorkBlock,
	}
	operation, ok := operations[action]
	if !ok {
		return fmt.Errorf("unknown foreground work action %q", action)
	}
	options, err := parseOptions("flow work "+action, arguments[1:], "", nil)
	if err != nil {
		return err
	}
	if options.programID == "" || options.entryID == "" || options.runID == "" || options.workID == "" {
		return fmt.Errorf("flow work %s requires --flow, --entry, --run-id, and --work-id", action)
	}
	bound, err := bindFlowEntry(context.Background(), options)
	if err != nil {
		return err
	}
	request, err := buildRequest(operation, bound)
	if err != nil {
		return err
	}
	if options.workQuestionSchemaPath != "" {
		request.WorkQuestionSchema, err = os.ReadFile(options.workQuestionSchemaPath)
		if err != nil {
			return err
		}
	}
	if options.workAnswerPath != "" {
		request.WorkAnswer, err = os.ReadFile(options.workAnswerPath)
		if err != nil {
			return err
		}
		if !json.Valid(request.WorkAnswer) {
			return fmt.Errorf("foreground work answer file is not JSON")
		}
	}
	kernel, err := standardKernel(context.Background(), request)
	if err != nil {
		return err
	}
	response, handleErr := kernel.Handle(context.Background(), request)
	if renderErr := renderResponse(response, options.format); renderErr != nil {
		return renderErr
	}
	return handleErr
}
