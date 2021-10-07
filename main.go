package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/costexplorer"
	"github.com/rs/zerolog/log"
	"github.com/slack-go/slack"
	"github.com/wcharczuk/go-chart"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"
)

var (
	SLACKENDPOINT = os.Getenv("SLACK_ENDPOINT")
	SLACKAPITOKEN = os.Getenv("SLACK_API_TOKEN")
	SLACKCHANNEL  = os.Getenv("SLACK_CHANNEL")
	AWSACCOUNT    = os.Getenv("AWS_ACCOUNT")
)

type Result struct {
	Name  string
	Cost  float64
	Ratio float64
}

func main() {
	lambda.Start(Handle) // lambdaで実行
	// ローカルで実行
	//err := Handle(context.Background())
	//if err != nil {
	//	log.Error().Msgf("error = %v", err)
	//}
}

func Handle(_ context.Context) error {
	start, end := getStartAndEndDay()
	log.Info().Msgf("start = %v, end = %v", start, end)
	// AWSの利用金額を取得する
	resp, err := getCost(start, end)
	if err != nil {
		return fmt.Errorf("get cost error = %w", err)
	}

	// 合計金額を出す
	totalCost := SumCost(resp)

	// 各サービスの金額を降順にまとめる
	list := makeCostList(resp, totalCost)

	// 利用料金計算の開始日、終了日、及び合計金額
	str := fmt.Sprintf("*AWS Account: %v*\n*Start: %v, End: %v*\n*Total Cost: $%.2f*\n", AWSACCOUNT, start, end, totalCost)

	// 各サービスの利用金額
	for _, v := range list {
		str = str + fmt.Sprintf("*%v*: %.2f(%.1f)%%\n", v.Name, v.Cost, v.Ratio)
	}
	str += "全体に占める利用料金の割合が1.0%未満のサービスは円グラフではOthersにまとめてあります"

	if err = sendToSlack(str); err != nil {
		return fmt.Errorf("send to slack error = %w", err)
	}

	b, err := drawPieGraph(list)
	if err != nil {
		return fmt.Errorf("draw pie graph error = %w", err)
	}

	if err = sendPieGraphToSlack(b); err != nil {
		return fmt.Errorf("send pie graph error = %w", err)
	}

	return nil
}

// 円グラフを描く
func drawPieGraph(results []Result) (*bytes.Buffer, error) {
	var values []chart.Value
	others := chart.Value{
		Label: "Others",
		Value: 0.0,
	}

	// グラフデータの作成
	for _, v := range results {
		// 全体に占める割合が1.0%未満のサービスはOthersにまとめる
		if v.Ratio < 1.0 {
			others.Value += v.Cost
			continue
		}
		values = append(values, chart.Value{
			Value: v.Cost,
			Label: v.Name,
		})
	}
	values = append(values, others)

	// グラフの作成
	pie := chart.PieChart{
		Width:  512,
		Height: 512,
		Values: values,
	}

	// バッファに画像データを書き込む
	buffer := bytes.NewBuffer([]byte{})
	err := pie.Render(chart.PNG, buffer)
	if err != nil {
		log.Error().Msgf("render error = %v", err)
		return nil, err
	}

	return buffer, nil
}

// Slackにテキストを送る
func sendToSlack(cost string) error {
	endpoint := SLACKENDPOINT

	body := struct {
		Text string `json:"text"`
	}{
		Text: fmt.Sprintf("*Monthly Report*\n %v", cost),
	}

	jsonString, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewBuffer(jsonString))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	client := new(http.Client)
	_, err = client.Do(req)
	if err != nil {
		return err
	}
	return nil
}

// Slackに円グラフを送る
func sendPieGraphToSlack(b *bytes.Buffer) error {
	api := slack.New(SLACKAPITOKEN)

	_, err := api.UploadFile(
		slack.FileUploadParameters{
			Reader:   b,
			Filename: "output.png",
			Channels: []string{SLACKCHANNEL},
		})
	if err != nil {
		return err
	}

	return nil
}

func getCost(start, end string) (*costexplorer.GetCostAndUsageOutput, error) {
	granularity := "MONTHLY"
	metrics := []string{"BlendedCost"}

	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1"),
	})
	if err != nil {
		return nil, err
	}

	svc := costexplorer.New(sess)

	result, err := svc.GetCostAndUsage(&costexplorer.GetCostAndUsageInput{
		TimePeriod: &costexplorer.DateInterval{
			Start: aws.String(start),
			End:   aws.String(end),
		},
		Granularity: aws.String(granularity),
		GroupBy: []*costexplorer.GroupDefinition{
			{
				Type: aws.String("DIMENSION"),
				Key:  aws.String("SERVICE"),
			},
		},
		Metrics: aws.StringSlice(metrics),
	})
	if err != nil {
		return nil, err
	}

	return result, err
}

func SumCost(cost *costexplorer.GetCostAndUsageOutput) float64 {
	sum := 0.0
	for _, v := range cost.ResultsByTime[0].Groups {
		x, _ := strconv.ParseFloat(*v.Metrics["BlendedCost"].Amount, 64)
		sum = sum + x
	}
	return sum
}

func makeCostList(cost *costexplorer.GetCostAndUsageOutput, totalCost float64) []Result {
	resp := make([]Result, 0, 0)
	for _, v := range cost.ResultsByTime[0].Groups {
		cost, _ := strconv.ParseFloat(*v.Metrics["BlendedCost"].Amount, 64)
		resp = append(resp, Result{
			Name:  *v.Keys[0],
			Cost:  cost,
			Ratio: cost / totalCost * 100,
		})
	}
	sort.Slice(resp, func(i, j int) bool {
		return resp[i].Cost > resp[j].Cost
	})

	return resp
}

func getStartAndEndDay() (string, string) {
	today := time.Now()
	lastMonthStartDay := time.Date(today.Year(), today.Month()-1, 1, 0, 0, 0, 0, time.Local)
	lastMonthEndDay := lastMonthStartDay.AddDate(0, 1, -1)
	start := lastMonthStartDay.Format("2006-01-02")
	end := lastMonthEndDay.Format("2006-01-02")

	return start, end
}
