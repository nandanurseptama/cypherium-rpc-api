const { default: axios } = require("axios");
const { getCypheriumPrice } = require("./externals");
const BigNumber = require("bignumber.js");

const bulkTokenBalance = async (req, res) => {
  console.log(req.body);
  var currency = req.body.currency;
  var cypheriumPrice = await getCypheriumPrice(currency.toLowerCase());
  var rpcBalanceParams = {
    jsonrpc: "2.0",
    method: "cph_getBalance",
    params: ["0x2dbde7263aaaf1286b9c41b1138191e178cb2fd4", "latest"],
    id: 1,
  };
  var log = {
    requestBody: req.body,
    endPoint: "/bulkTokenBalance",
    time: Date().toString(),
  };
  console.log(log);
  var body = req.body.data;
  var response = [];
  for (var i = 0; i < body.length; i++) {
    if (body[i].contractAddress === null) {
      var params = rpcBalanceParams;
      params.params = [body[i].address, "latest"];
      console.log(params);
      var resAxios = await axios.post(`${process.env.RPC_API}`, params, {
        headers: { "Content-Type": "application/json" },
      });
      console.log(resAxios.data);
      if (resAxios.status != 200) {
        return res.status(500).json({
          message: resAxios.status,
          data: null,
        });
      }
      var ui_balance = BigNumber(resAxios.data.result)
        .dividedBy(BigNumber(Math.pow(10, 18)))
        .toFixed();
      console.log(cypheriumPrice);
      response.push({
        balance: BigNumber.BigNumber(resAxios.data.result).toFixed(),
        ui_balance: ui_balance,
        money_balance: parseFloat(ui_balance) * cypheriumPrice,
        address : body[i].address,
        contractAddress : body[i].contractAddress,
      });
    }
  }
  return res.  status(200).json(response);
};
module.exports = accountsEndpoint = {
  bulkTokenBalance,
};
