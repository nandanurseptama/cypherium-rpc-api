const { default: axios } = require("axios");
const { getCypheriumPrice } = require("./externals");
const BigNumber = require("bignumber.js");
var Web3c = require('@cypherium/web3c');
var tokenAbi = require('../../constant/abis/tokenAbi.json');
const bulkTokenBalance = async (req, res) => {
  var apiUrl = req.query.network === 'mainnet' ? process.env.RPC_API : process.env.RPC_API_TESTNET;
  var web3c = new Web3c(
    new Web3c.providers.HttpProvider(
      apiUrl,
      null,
      null,
      null,
      [
        {
          name: "Authorization",
          value: "kla7771ksdasbc===",
        },
      ]
    )
  );
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
      var resAxios = await axios.post(apiUrl, params, {
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
        .dividedBy(BigNumber(Math.pow(10, body[i].decimal)))
        .toFixed();
      console.log(cypheriumPrice);
      response.push({
        balance: BigNumber.BigNumber(resAxios.data.result).toFixed(),
        ui_balance: ui_balance,
        money_balance: parseFloat(ui_balance) * cypheriumPrice,
        address : body[i].address,
        contractAddress : body[i].contractAddress,
        name : "Cypherium",
        decimal : body[i].decimal,
        symbol : "CPH",
      });
    } else{
      var tokenContract = web3c.cph.contract(tokenAbi).at(body[i].contractAddress);
      var balanceOf = await tokenContract.balanceOf([
        body[i].address
      ]);
      var symbol = await tokenContract.symbol();
      var name = await tokenContract.name();
      var decimal = await tokenContract.decimals();
      var ui_balance = BigNumber(balanceOf)
        .dividedBy(BigNumber(Math.pow(10, decimal)))
        .toFixed();
      response.push({
        balance: BigNumber.BigNumber(balanceOf).toFixed(),
        ui_balance: ui_balance,
        money_balance: parseFloat(ui_balance) * 0,
        address : body[i].address,
        contractAddress : body[i].contractAddress,
        name : name,
        decimal : decimal,
        symbol : symbol,
      });
    }
  }
  return res.status(200).json(response);
};
module.exports = accountsEndpoint = {
  bulkTokenBalance,
};
