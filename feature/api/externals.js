const { default: axios } = require("axios");

const getCypheriumPrice = async (currency) => {
  try {
    var headers = new Array();
    headers["accept"] = "application/json";
    var res = await axios.get(
      `https://api.coingecko.com/api/v3/simple/price?ids=cypherium&vs_currencies=${currency}`,
      {
        headers: headers,
      }
    );
    var data = await res.data;
    if (res.status != 200) {
      throw res.statusText;
    }
    console.log(data);
    return data.cypherium[currency];
  } catch (e) {
    throw e;
  }
};

module.exports = {
    getCypheriumPrice
}